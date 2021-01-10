package db

type File struct {
	Hash      string    `json:"hash"`
	Date      time.Time `json:"date"`
	Thumbnail []byte    `json:"thumbnail"`
	// Found maps backend ID to sorted list of file IDs in that backend
	Found map[string][]string `json:"found"`
}

type fileDoc struct {
	Hash      string      `json:"hash"`
	Date      time.Time   `json:"date"`
	Thumbnail string      `json:"thumbnail"`
	Found     interface{} `json:"found"`
}
