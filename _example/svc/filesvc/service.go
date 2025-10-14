package filesvc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"sync"

	"connectrpc.com/connect"
	"github.com/humbornjo/mizu/mizuconnect/restful/upload"

	filev1 "mizu.example/protogen/app_foo/file/v1"
	"mizu.example/protogen/app_foo/file/v1/filev1connect"
)

type Service struct {
	storage sync.Map
}

var _ filev1connect.FileServiceHandler = (*Service)(nil)

const FILE_FIELD = "file"

func NewService() filev1connect.FileServiceHandler {
	return &Service{}
}

func (s *Service) GetFile(ctx context.Context, req *connect.Request[filev1.GetFileRequest],
) (*connect.Response[filev1.GetFileResponse], error) {
	id := req.Msg.GetId()

	data, ok := s.storage.Load(id)
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, nil)
	}

	bytes, ok := data.([]byte)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, nil)
	}
	return connect.NewResponse(&filev1.GetFileResponse{Url: "http://" + hex.EncodeToString(bytes[:])}), nil
}

func (s *Service) UploadFile(ctx context.Context, stream *connect.ClientStream[filev1.UploadFileRequest],
) (*connect.Response[filev1.UploadFileResponse], error) {
	msg := filev1.UploadFileRequest{}
	rxForm, err := upload.NewFormReader(FILE_FIELD, stream, &msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	var url, checksum string
	for {
		part, err := rxForm.NextPart()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		if part.FormName() == FILE_FIELD {
			rxFile := upload.NewFileReader(part, upload.WithLimitBytes(1024*64))
			defer rxFile.Close() // nolint: errcheck
			url, err = s.uploadFile(rxFile)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, err)
			}
			checksum = rxFile.Checksum()
			slog.InfoContext(
				ctx, "file uploaded", "url", url, "checksum", checksum,
				"content-type", rxFile.ContentType(), "file-size", rxFile.ReadSize(),
			)
		}
	}

	return connect.NewResponse(&filev1.UploadFileResponse{Id: checksum, Url: url}), nil
}

func (s *Service) uploadFile(file io.ReadCloser) (string, error) {
	hash := sha256.New()

	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	id := hex.EncodeToString(hash.Sum(nil))
	s.storage.Store(id, hash.Sum(nil))
	return "http://" + id, nil
}
