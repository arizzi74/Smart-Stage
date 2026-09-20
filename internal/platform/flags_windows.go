//go:build windows && cgo

package platform

/*
#cgo CXXFLAGS: -std=c++17 -D_WIN32_WINNT=0x0A00 -DUNICODE -D_UNICODE -Wall -Wextra -I${SRCDIR}/webview2
#cgo LDFLAGS: -static-libstdc++ -static-libgcc -Wl,-Bstatic -lwinpthread -Wl,-Bdynamic -lmfplat -lmf -lmfuuid -lmfreadwrite -levr -lole32 -loleaut32 -luuid -lpropsys -luser32 -lgdi32 -lshcore -lshell32 -ldcomp -lbcrypt -ladvapi32
*/
import "C"
