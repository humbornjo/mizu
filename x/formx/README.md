# formx - Streaming Multipart Forms for Go

Stream multipart uploads while decoding declared form fields into a typed struct, built on Go's standard `mime/multipart` package.

## Features

- **Streaming-First** - The file part is never buffered; fields are decoded around it
- **Typed Fields** - Declared fields map onto a struct as parts arrive
- **File Inspection** - Size limit, SHA-256 checksum, and MIME sniffing on the stream
- **Lenient Mapping** - Unconvertible or oversized fields are skipped, never fatal

## Installation

```bash
go get github.com/humbornjo/mizu/x/formx
```

## Quick Start

```go
package main

import (
    "io"
    "log"
    "net/http"

    "github.com/humbornjo/mizu/x/formx"
)

type UploadForm struct {
    Name     string `form:"name"`
    Scenario *int   `form:"scenario"`
}

func upload(w http.ResponseWriter, r *http.Request) {
    var fields UploadForm
    form, err := formx.NewFormReader("package", r, &fields)
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    defer form.Close()

    part, purge, err := form.File()
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    file := formx.NewFileReader(part, formx.WithFileLimitBytes(64<<20))
    defer file.Close()

    if _, err := io.Copy(io.Discard, file); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    if err := purge(); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    log.Printf("name=%s sha256=%s", fields.Name, file.Checksum())
    w.WriteHeader(http.StatusCreated)
}
```

Fields may appear before or after the file part; call `purge` after consuming the file to decode trailing fields.

## Field Mapping

Field names resolve from `form` tags, then `json` tags, then Go field names. `-` in either tag ignores the field. Mapping is best-effort: an unconvertible part leaves its field untouched, bytes beyond the field limit are discarded, and a repeated field keeps the last value.

Supported field types: strings, bools, integers, unsigned integers, floats, `[]byte`, pointers to any of these, and types implementing `encoding.TextUnmarshaler`.

Unknown parts are returned by `NextPart` untouched. Pass a nil message to handle every part manually:

```go
form, err := formx.NewFormReader("package", r, nil)
for {
    part, err := form.NextPart()
    if errors.Is(err, io.EOF) {
        break
    }
    // handle part yourself
}
```

## File Inspection

`NewFileReader` wraps any `io.ReadCloser` with bookkeeping on the stream:

| Method         | Description                                       |
| -------------- | ------------------------------------------------- |
| `ReadSize()`   | Bytes read so far                                 |
| `Checksum()`   | SHA-256 of the bytes read so far, hex encoded     |
| `ContentType()`| MIME type sniffed from the first 512 bytes        |
| `MimeSniffer()`| Copy of the bytes used for MIME detection         |

Reading past the configured limit returns `formx.ErrFileTooLarge`.

## Configuration Options

| Option                     | Description                                                  | Default        |
| -------------------------- | ------------------------------------------------------------ | -------------- |
| `WithFormFieldLimitBytes`  | Max bytes read per declared field; the excess is discarded   | `4096`         |
| `WithFileLimitBytes`       | Max bytes read from the file before `ErrFileTooLarge`        | unlimited      |
