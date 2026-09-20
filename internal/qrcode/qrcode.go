// Package qrcode renders local pairing links without external services or
// runtime dependencies. An integer scale and four-module quiet zone keep the
// original PNG sharp for a phone camera.
package qrcode

import (
	_ "embed"
	"errors"

	qr "github.com/piglig/go-qr"
)

//go:embed licenses.txt
var licenses string

// Licenses supplies the notice from the running executable, including when its
// distribution contains no companion files.
func Licenses() string { return licenses }

func PNG(text string) ([]byte, error) {
	if len(text) == 0 || len(text) > 1024 {
		return nil, errors.New("invalid pairing QR payload length")
	}
	code, err := qr.EncodeText(text, qr.Medium)
	if err != nil {
		return nil, err
	}
	return code.ToPNGBytes(qr.NewQrCodeImgConfig(8, 4))
}
