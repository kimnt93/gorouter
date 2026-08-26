package llm

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

type awsEvent struct {
	Type    string
	Payload any
}

func readAWSEvents(reader io.Reader, visit func(awsEvent) error) error {
	buffered := bufio.NewReader(reader)
	for {
		prelude := make([]byte, 12)
		if _, err := io.ReadFull(buffered, prelude); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			return err
		}
		total := int(binary.BigEndian.Uint32(prelude[0:4]))
		headersLength := int(binary.BigEndian.Uint32(prelude[4:8]))
		if total < 16 || total > 16<<20 || headersLength < 0 || headersLength > total-16 {
			return fmt.Errorf("invalid AWS event-stream frame")
		}
		remaining := make([]byte, total-12)
		if _, err := io.ReadFull(buffered, remaining); err != nil {
			return err
		}
		headers := remaining[:headersLength]
		payload := remaining[headersLength : len(remaining)-4]
		eventType := awsHeader(headers, ":event-type")
		var value any
		if len(payload) > 0 && json.Unmarshal(payload, &value) != nil {
			continue
		}
		if err := visit(awsEvent{Type: eventType, Payload: value}); err != nil {
			return err
		}
	}
}

func awsHeader(headers []byte, want string) string {
	for offset := 0; offset < len(headers); {
		nameLen := int(headers[offset])
		offset++
		if offset+nameLen+1 > len(headers) {
			return ""
		}
		name := string(headers[offset : offset+nameLen])
		offset += nameLen
		kind := headers[offset]
		offset++
		switch kind {
		case 7:
			if offset+2 > len(headers) {
				return ""
			}
			size := int(binary.BigEndian.Uint16(headers[offset : offset+2]))
			offset += 2
			if offset+size > len(headers) {
				return ""
			}
			value := string(headers[offset : offset+size])
			offset += size
			if name == want {
				return value
			}
		case 0, 1:
		case 2:
			offset++
		case 3:
			offset += 2
		case 4:
			offset += 4
		case 5, 8:
			offset += 8
		case 6:
			if offset+2 > len(headers) {
				return ""
			}
			size := int(binary.BigEndian.Uint16(headers[offset : offset+2]))
			offset += 2 + size
		case 9:
			offset += 16
		default:
			return ""
		}
	}
	return ""
}
