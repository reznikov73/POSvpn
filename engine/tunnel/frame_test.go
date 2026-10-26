package tunnel

import (
	"errors"
	"testing"
)

func TestHeaderRoundTrip(t *testing.T) {
	want := Header{
		Version: Version1,
		Type:    FrameIPv4,
		Dst:     42,
		Src:     7,
		Epoch:   0xDEADBEEF,
		Counter: 1 << 40,
	}
	buf := make([]byte, HeaderSize)
	want.EncodeTo(buf)

	got, err := ParseHeader(buf)
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	if got != want {
		t.Fatalf("заголовок не совпал:\n got %+v\nwant %+v", got, want)
	}
}

func TestHeaderRejects(t *testing.T) {
	cases := []struct {
		name string
		buf  []byte
		want error
	}{
		{"короткий", make([]byte, HeaderSize-1), ErrShortFrame},
		{"версия", headerWith(t, func(h *Header) { h.Version = 9 }), ErrFrameVersion},
		{"тип", headerWith(t, func(h *Header) { h.Type = 99 }), ErrFrameType},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseHeader(tc.buf); !errors.Is(err, tc.want) {
				t.Fatalf("получили %v, ждали %v", err, tc.want)
			}
		})
	}
}

func TestSetDstAndSrc(t *testing.T) {
	buf := make([]byte, HeaderSize)
	Header{Version: Version1, Type: FrameIPv4, Dst: 1, Src: 1}.EncodeTo(buf)

	SetDst(buf, BroadcastIndex)
	SetSrc(buf, 77)

	got, err := ParseHeader(buf)
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	if got.Dst != BroadcastIndex || got.Src != 77 {
		t.Fatalf("после правки: dst=%d src=%d", got.Dst, got.Src)
	}
}

func TestFrameTypeIsData(t *testing.T) {
	if !FrameIPv4.IsData() || !FrameIPv6.IsData() {
		t.Fatal("IP-кадры должны считаться данными")
	}
	if FramePing.IsData() || FramePong.IsData() {
		t.Fatal("служебные кадры не должны считаться данными")
	}
}

func headerWith(t *testing.T, mutate func(*Header)) []byte {
	t.Helper()
	buf := make([]byte, HeaderSize)
	h := Header{Version: Version1, Type: FrameIPv4, Dst: 2, Src: 1}
	mutate(&h)
	h.EncodeTo(buf)
	return buf
}
