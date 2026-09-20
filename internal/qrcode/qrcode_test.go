package qrcode

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	qr "github.com/piglig/go-qr"
)

func TestEmbeddedLicenseIncludesCompleteUpstreamNotice(t *testing.T) {
	upstream, err := os.ReadFile(filepath.Join("..", "..", "vendor", "github.com", "piglig", "go-qr", "LICENSE"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(Licenses(), string(upstream)) {
		t.Fatal("the one-executable distribution omits part of its QR dependency's license")
	}
}

func TestPairingQRRoundTrip(t *testing.T) {
	for _, link := range []string{
		"http://192.168.1.28:8788/command#token=00123456",
		"http://[2001:db8::1234]:8788/command#token=98765432",
		"http://169.254.200.200:65535/command#token=00000000",
	} {
		t.Run(link, func(t *testing.T) {
			data, err := PNG(link)
			if err != nil {
				t.Fatal(err)
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := qr.Decode(img)
			if err != nil || decoded != link {
				t.Fatalf("camera payload = %q, %v; want %q", decoded, err, link)
			}
			for _, x := range []int{0, 31, img.Bounds().Max.X - 1} {
				for y := 0; y < img.Bounds().Max.Y; y++ {
					r, g, b, _ := img.At(x, y).RGBA()
					if r != 65535 || g != 65535 || b != 65535 {
						t.Fatal("QR quiet zone is not four white modules")
					}
				}
			}
		})
	}
	for _, invalid := range []string{"", strings.Repeat("x", 1025)} {
		if _, err := PNG(invalid); err == nil {
			t.Fatal("unbounded QR payload accepted")
		}
	}
}
