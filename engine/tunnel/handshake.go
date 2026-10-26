package tunnel

// handshakeVersion — версия рукопожатия поверх первого двунаправленного
// потока QUIC. Растёт отдельно от версии кадра.
const handshakeVersion = 1

// helloRequest — первое сообщение клиента: версия и токен узла.
type helloRequest struct {
	Version int    `json:"version"`
	Token   string `json:"token"`
}

// helloResponse — ответ хаба. OK=false означает отказ, текст — причина.
type helloResponse struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	NodeID string `json:"node_id,omitempty"`
}
