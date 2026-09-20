//go:build darwin && cgo

package platform

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -fblocks -mmacosx-version-min=12.0 -Wall -Wextra
#cgo LDFLAGS: -mmacosx-version-min=12.0 -framework AppKit -framework AVFoundation -framework CoreAudio -framework CoreMedia -framework CoreVideo -framework QuartzCore -framework CoreGraphics -framework IOKit -framework UniformTypeIdentifiers -framework WebKit -framework ImageIO
*/
import "C"
