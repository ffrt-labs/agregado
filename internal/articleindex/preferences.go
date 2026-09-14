package articleindex

import "os"

type FilePreferences struct{ Path string }

func (p FilePreferences) Read() (string, error) {
	contents, err := os.ReadFile(p.Path)
	if err != nil {
		return "", err
	}
	return string(contents), nil
}
