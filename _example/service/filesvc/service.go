package filesvc

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"io"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/humbornjo/mizu/mizuconnect/restful/filekit"
	"google.golang.org/genproto/googleapis/api/httpbody"

	filev1 "mizu.example/protogen/barapp/file/v1"
	"mizu.example/protogen/barapp/file/v1/filev1connect"
)

type Service struct {
	storage Storage
}

var _ filev1connect.FileServiceHandler = (*Service)(nil)

var (
	//go:embed file.json
	FILE_DATA []byte
	FILE_MIME = "application/json"

	FILE_FIELD = "file"
)

func NewService() filev1connect.FileServiceHandler {
	return &Service{storage: NewStorage()}
}

func (s *Service) GetFile(ctx context.Context, req *connect.Request[filev1.GetFileRequest],
) (*connect.Response[filev1.GetFileResponse], error) {
	id := req.Msg.GetId()

	file, err := s.storage.Retrieve(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, nil)
	}

	return connect.NewResponse(&filev1.GetFileResponse{Url: s.genPublicUrl(file.Checksum())}), nil
}

func (s *Service) UploadFile(ctx context.Context, stream *connect.ClientStream[filev1.UploadFileRequest],
) (*connect.Response[filev1.UploadFileResponse], error) {
	msg := filev1.UploadFileRequest{}
	rxForm, err := filekit.NewFormReader(
		FILE_FIELD, stream, &msg,
		filekit.WithFormProtoMode[*filev1.UploadFileRequest](filekit.MODE_PROTO_HYBRID),
	)
	if err != nil {
		slog.ErrorContext(ctx, "failed create form reader", "err", err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	fpart, purge, err := rxForm.File()
	if err != nil {
		slog.ErrorContext(ctx, "failed get file part", "err", err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	rxFile := filekit.NewFileReader(fpart, filekit.WithFileLimitBytes(64*1024*1024))
	id, err := s.storage.Store(ctx, rxFile)
	if err != nil {
		slog.ErrorContext(ctx, "failed store file", "err", err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	if err := purge(); err != nil {
		slog.ErrorContext(ctx, "failed drain form data", "err", err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	url := s.genPublicUrl(rxFile.Checksum())
	slog.InfoContext(
		ctx, "file uploaded",
		"id", id, "checksum", rxFile.Checksum(), "scenario", msg.GetScenario(),
		"content-type", rxFile.ContentType(), "file-size", rxFile.ReadSize(),
	)

	return connect.NewResponse(&filev1.UploadFileResponse{Id: id, Url: url}), nil
}

func (s *Service) DownloadFile(
	ctx context.Context, req *connect.Request[filev1.DownloadFileRequest], stream *connect.ServerStream[httpbody.HttpBody],
) error {
	id := req.Msg.GetId()
	file, err := s.storage.Retrieve(ctx, id)
	if err == nil {
		txFile, err := filekit.NewWriter(stream, &httpbody.HttpBody{ContentType: file.ContentType()})
		if err != nil {
			slog.ErrorContext(ctx, "failed to create writer", "err", err)
			return connect.NewError(connect.CodeInternal, err)
		}
		defer txFile.Close() // nolint: errcheck
		nbyte, err := io.Copy(txFile, file)
		slog.DebugContext(ctx, "file send", "nbyte", nbyte, "err", err)
		return err
	}
	slog.WarnContext(ctx, "file not found, deliver default content", "id", id, "err", err)

	txFile, err := filekit.NewWriter(stream, &httpbody.HttpBody{ContentType: FILE_MIME})
	if err != nil {
		slog.ErrorContext(ctx, "failed to create writer", "err", err)
		return connect.NewError(connect.CodeInternal, err)
	}
	defer txFile.Close() // nolint: errcheck

	nbyte, err := io.Copy(txFile, bytes.NewReader(FILE_DATA))
	slog.DebugContext(ctx, "file send", "nbyte", nbyte, "err", err)
	return err
}

func (s *Service) genPublicUrl(checksum string) string {
	return "http://supercool/" + base64.URLEncoding.EncodeToString([]byte(checksum))
}
