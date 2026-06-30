package conf

import (
	"bufio"
	"io"
	"os"
)

// NewEnvExpandedReader wraps origin so $VAR / ${VAR} references in the YAML are
// expanded from the environment as it is read (e.g. name: $INSTANCE_NAME).
func NewEnvExpandedReader(origin io.Reader) io.Reader {
	return &envExpandedReader{
		bufio.NewReader(origin),
		make([]byte, 0),
	}
}

type envExpandedReader struct {
	origin         *bufio.Reader
	remainingBytes []byte
}

func (r *envExpandedReader) Read(p []byte) (n int, err error) {
	for len(r.remainingBytes) <= len(p) {
		var line string
		line, err = r.origin.ReadString('\n')
		if err != nil && err != io.EOF {
			return 0, err
		}

		out := os.ExpandEnv(line)
		r.remainingBytes = append(r.remainingBytes, []byte(out)...)

		if err == io.EOF {
			break
		}
	}

	n = copy(p, r.remainingBytes)
	r.remainingBytes = r.remainingBytes[n:]
	return
}
