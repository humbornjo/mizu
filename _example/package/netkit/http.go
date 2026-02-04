package netkit

import "net/http"

func WriteString(w http.ResponseWriter, s string, code int) error {
	w.WriteHeader(code)
	_, err := w.Write([]byte(s))
	return err
}
