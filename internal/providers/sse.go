package providers

import (
	"bufio"
	"bytes"
	"io"
	"strings"
)

// SSEEvent represents a Server-Sent Event.
type SSEEvent struct {
	Event string
	Data  string
	ID    string
}

// SSEReader reads Server-Sent Events from a stream.
type SSEReader struct {
	scanner *bufio.Scanner
}

// NewSSEReader creates a new SSE reader.
func NewSSEReader(r io.Reader) *SSEReader {
	scanner := bufio.NewScanner(r)
	scanner.Split(splitSSE)
	return &SSEReader{scanner: scanner}
}

// Next reads the next SSE event.
func (r *SSEReader) Next() (*SSEEvent, error) {
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}

	return parseSSEEvent(r.scanner.Bytes()), nil
}

// splitSSE is a split function for bufio.Scanner that splits on double newlines.
func splitSSE(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	// Look for double newline (event boundary)
	if i := bytes.Index(data, []byte("\n\n")); i >= 0 {
		return i + 2, data[0:i], nil
	}

	// Also handle \r\n\r\n
	if i := bytes.Index(data, []byte("\r\n\r\n")); i >= 0 {
		return i + 4, data[0:i], nil
	}

	// If at EOF, return remaining data
	if atEOF {
		return len(data), data, nil
	}

	// Request more data
	return 0, nil, nil
}

// parseSSEEvent parses an SSE event from raw bytes.
func parseSSEEvent(data []byte) *SSEEvent {
	event := &SSEEvent{}
	lines := bytes.Split(data, []byte("\n"))

	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		// Handle lines starting with ':'  (comments)
		if line[0] == ':' {
			continue
		}

		// Split on first colon
		parts := bytes.SplitN(line, []byte(":"), 2)
		if len(parts) != 2 {
			continue
		}

		field := string(bytes.TrimSpace(parts[0]))
		value := string(bytes.TrimSpace(parts[1]))

		switch field {
		case "event":
			event.Event = value
		case "data":
			if event.Data != "" {
				event.Data += "\n"
			}
			event.Data += value
		case "id":
			event.ID = value
		}
	}

	// Trim "data: " prefix if present (some APIs include it)
	event.Data = strings.TrimPrefix(event.Data, "data: ")

	return event
}
