package httputils

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/grafana/gcx/internal/logs"
	"github.com/grafana/grafana-app-sdk/logging"
)

func Error(r *http.Request, w http.ResponseWriter, msg string, err error, code int) {
	logging.FromContext(r.Context()).Warn(fmt.Sprintf("%d - %s: %s", code, msg, err.Error()), logs.Err(err))
	http.Error(w, msg, code)
}

func Write(r *http.Request, w http.ResponseWriter, content []byte) {
	if _, err := w.Write(content); err != nil {
		logging.FromContext(r.Context()).Error("error writing HTTP response", logs.Err(err))
	}
}

func WriteJSON(r *http.Request, w http.ResponseWriter, content any) {
	responseJSON, err := json.Marshal(content)
	if err != nil {
		logging.FromContext(r.Context()).Error("error marshalling HTTP response to JSON", logs.Err(err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	Write(r, w, responseJSON)
}
