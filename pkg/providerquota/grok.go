package providerquota

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

// Grok Build publishes one shared weekly credit pool via gRPC-web. Unknown
// responses must never be interpreted as a zero remaining balance.
func (s *Service) grokCredits(ctx context.Context, runtime *entities.CredentialRuntime) (Window, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://grok.com/grok_api_v2.GrokBuildBilling/GetGrokCreditsConfig", bytes.NewReader([]byte{0, 0, 0, 0, 0}))
	if err != nil {
		return Window{}, err
	}
	req.Header.Set("Authorization", "Bearer "+runtime.OAuthAccess)
	req.Header.Set("Content-Type", "application/grpc-web+proto")
	req.Header.Set("X-Grpc-Web", "1")
	resp, err := s.client.Do(req)
	if err != nil {
		return Window{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Window{}, errors.New("Grok quota endpoint unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return Window{}, err
	}
	if len(raw) >= 5 && raw[0] == 0 && int(binary.BigEndian.Uint32(raw[1:5])) <= len(raw)-5 {
		raw = raw[5 : 5+binary.BigEndian.Uint32(raw[1:5])]
	}
	fields, ok := protoFields(raw)
	if !ok {
		return Window{}, errors.New("invalid Grok quota response")
	}
	info, ok := fields[1]
	if !ok || info.wire != 2 {
		return Window{}, errors.New("Grok quota response omitted credits")
	}
	credits, ok := protoFields(info.data)
	if !ok {
		return Window{}, errors.New("invalid Grok credits")
	}
	ratio := float64(0)
	if v, found := credits[1]; found {
		if v.wire != 5 || len(v.data) != 4 {
			return Window{}, errors.New("invalid Grok credits ratio")
		}
		ratio = float64(math.Float32frombits(binary.LittleEndian.Uint32(v.data)))
	}
	if math.IsNaN(ratio) || ratio < 0 || ratio > 1.01 {
		return Window{}, errors.New("invalid Grok credits ratio")
	}
	var reset *time.Time
	if v, found := credits[5]; found && v.wire == 2 {
		if ts, valid := protoFields(v.data); valid {
			if sec, found := ts[1]; found && sec.wire == 0 && sec.number > 0 {
				t := time.Unix(int64(sec.number), 0).UTC()
				reset = &t
			}
		}
	}
	return percentWindow("Weekly credits", ratio*100, reset), nil
}

type protoField struct {
	wire   uint64
	data   []byte
	number uint64
}

func varint(b []byte) (uint64, int, bool) {
	var v uint64
	for i := 0; i < len(b) && i < 10; i++ {
		if i == 9 && b[i] > 1 {
			return 0, 0, false
		}
		v |= uint64(b[i]&127) << (7 * i)
		if b[i] < 128 {
			return v, i + 1, true
		}
	}
	return 0, 0, false
}
func protoFields(raw []byte) (map[uint64]protoField, bool) {
	out := map[uint64]protoField{}
	for len(raw) > 0 {
		key, n, ok := varint(raw)
		if !ok || key>>3 == 0 {
			return nil, false
		}
		raw = raw[n:]
		field := protoField{wire: key & 7}
		switch field.wire {
		case 0:
			field.number, n, ok = varint(raw)
			if !ok {
				return nil, false
			}
			raw = raw[n:]
		case 1, 5:
			width := 8
			if field.wire == 5 {
				width = 4
			}
			if len(raw) < width {
				return nil, false
			}
			field.data = raw[:width]
			raw = raw[width:]
		case 2:
			size, count, valid := varint(raw)
			if !valid || size > uint64(len(raw)-count) {
				return nil, false
			}
			field.data = raw[count : count+int(size)]
			raw = raw[count+int(size):]
		default:
			return nil, false
		}
		out[key>>3] = field
	}
	return out, true
}
