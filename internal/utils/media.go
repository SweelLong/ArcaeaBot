package utils

import (
	"encoding/base64"
	"os"
)

func Base64File(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return "base64://" + base64.StdEncoding.EncodeToString(raw), nil
}
