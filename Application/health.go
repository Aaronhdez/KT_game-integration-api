package application

import "net/http"

type healthResponse struct {
	Status string `json:"status"`
}

// Health reports that the service is up. It is a liveness check only:
// it does not check the database or the Wallet.
func Health(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, healthResponse{
		Status: "ok",
	})
}
