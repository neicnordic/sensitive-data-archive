package main

import (
	"net/http"
)

func (api *API) routes() http.Handler {
	// routes with trailing slashes act as prefix matchers (Go 1.22+ ServeMux semantics).

	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("GET /ready", api.readinessResponse)

	// Files
	mux.HandleFunc("GET /files", api.rbac(api.getFiles))
	mux.HandleFunc("POST /file/ingest", api.rbac(api.ingestFile))
	mux.HandleFunc("POST /file/accession", api.rbac(api.setAccession))
	mux.HandleFunc("PUT /file/verify/{accession}", api.rbac(api.reVerifyFile))
	mux.HandleFunc("POST /file/rotatekey/{fileid}", api.rbac(api.rotateKeyFile))
	mux.HandleFunc("DELETE /file/{username}/{fileid}", api.rbac(api.deleteFile))

	// Datasets
	mux.HandleFunc("GET /datasets", api.rbac(api.listDatasets))
	mux.HandleFunc("GET /datasets/list", api.rbac(api.listAllDatasets))
	mux.HandleFunc("GET /datasets/list/{username}", api.rbac(api.listUserDatasets))
	mux.HandleFunc("POST /dataset/create", api.rbac(api.createDataset))
	mux.HandleFunc("POST /dataset/rotatekey/{dataset}", api.rbac(api.rotateKeyDataset))
	mux.HandleFunc("POST /dataset/release/", api.rbac(api.releaseDataset))
	mux.HandleFunc("PUT /dataset/verify/", api.rbac(api.reVerifyDataset))

	// Users
	mux.HandleFunc("GET /users", api.rbac(api.listActiveUsers))
	mux.HandleFunc("GET /users/{username}/files", api.rbac(api.listUserFiles))
	mux.HandleFunc("GET /users/{username}/file/{fileid}", api.rbac(api.downloadFile))

	// C4GH Keys
	mux.HandleFunc("GET /c4gh-keys/list", api.rbac(api.listC4ghHashes))
	mux.HandleFunc("POST /c4gh-keys/add", api.rbac(api.addC4ghHash))
	mux.HandleFunc("POST /c4gh-keys/deprecate/", api.rbac(api.deprecateC4ghHash))

	var handler http.Handler = mux
	handler = api.recoveryMiddleware(handler)
	handler = api.loggingMiddleware(handler)
	return handler
}
