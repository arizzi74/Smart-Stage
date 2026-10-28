// Package cloudflared runs a pinned, isolated Cloudflare Quick Tunnel helper.
// It never discovers a system installation or uses an account or tunnel token.
package cloudflared

import "runtime"

const Version = "2026.10.5"

const releaseBase = "https://github.com/cloudflare/cloudflared/releases/download/" + Version + "/"

type asset struct {
	name   string
	sha256 string
	size   int64
	tgz    bool
}

// These are the official GitHub release asset digests, checked on 2026-10-28:
// https://api.github.com/repos/cloudflare/cloudflared/releases/tags/2026.10.5
// https://github.com/cloudflare/cloudflared/releases/tag/2026.10.5
// The Darwin archives were replaced after release; their current asset digests
// (also verified against downloaded bytes) supersede the stale release-body sums.
// Release discovery and GitHub API requests are deliberately absent at runtime.
var nativeAssets = map[string]asset{
	"darwin/amd64":  {"cloudflared-darwin-amd64.tgz", "6699392fed92367808c248c2d90bed66e49bbf66e1b4ee3e4141230f3f7391b3", 21798374, true},
	"darwin/arm64":  {"cloudflared-darwin-arm64.tgz", "184c7d4f5f977348243a6d7ca8f6cf6073b9b2746cfe8c1fe15aa59e9a522bf0", 19840425, true},
	"linux/386":     {"cloudflared-linux-386", "f2de713829081837130b38e27f3c95aa9d7fc1a4f48b833362d21234dd8780d8", 37680533, false},
	"linux/amd64":   {"cloudflared-linux-amd64", "e1bc0cf084fd9e590554fe769a86b1db46701c3f847a43d902f7b974a4e16298", 40263303, false},
	"linux/arm":     {"cloudflared-linux-arm", "a018911e1bbfd739937f3011f589e9e73245baeee14742f8b3413472bb007adc", 37132381, false},
	"linux/arm64":   {"cloudflared-linux-arm64", "72e0d05a67e09da87cf495e409a28c3cc5994dd23fc66a982eebea6a55923b7d", 37662975, false},
	"windows/386":   {"cloudflared-windows-386.exe", "66cba8003e4f87a8e6b772f331b8c04c7d41ac4f2dc30dd2c13ae09de3b6007e", 38078600, false},
	"windows/amd64": {"cloudflared-windows-amd64.exe", "db4e4c413612a0916f3e1679d3caaf376f94911952d163028fb726fb2e0516a9", 55343328, false},
}

// Available reports whether the pinned release has a native helper for this
// process's OS and architecture. Emulation and PATH fallbacks are never used.
func Available() bool {
	ok, _ := Support()
	return ok
}

// Support returns availability and a user-facing explanation when unavailable.
func Support() (bool, string) { return platformSupport(runtime.GOOS, runtime.GOARCH) }

func platformSupport(goos, goarch string) (bool, string) {
	if _, ok := nativeAssets[goos+"/"+goarch]; ok {
		return true, ""
	}
	if goos == "windows" && goarch == "arm64" {
		return false, "Cloudflare is unavailable on Windows ARM64 because Cloudflare does not publish a native cloudflared helper for this platform. Choose Public gateway or Local network."
	}
	return false, "Cloudflare is unavailable because Cloudflare does not publish a native cloudflared helper for this platform. Choose Public gateway or Local network."
}
