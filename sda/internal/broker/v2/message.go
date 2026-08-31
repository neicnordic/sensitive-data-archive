package v2

type Message struct {
	Key     string
	Headers map[string]any
	Body    []byte
}
