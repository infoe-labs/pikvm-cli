package pikvm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// CopyProgress relays every NDJSON progress record and detects late API failures
// even after the initial HTTP response succeeded. It preserves the response bytes.
func CopyProgress(dst io.Writer, src io.Reader) error {
	reader := bufio.NewReader(src)
	failed := false
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if _, e := dst.Write(line); e != nil {
				return e
			}
			var record struct {
				OK *bool `json:"ok"`
			}
			if json.Unmarshal(line, &record) == nil && record.OK != nil && !*record.OK {
				failed = true
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if failed {
		return fmt.Errorf("PiKVM streaming operation failed; see response records")
	}
	return nil
}
