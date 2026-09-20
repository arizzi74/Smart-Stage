# Application icon

Original Smart Stage artwork in `smartstage.svg`, using the interface's dark
background and mint accent. `smartstage.png`, `.ico` and `.icns` are generated
from that vector source and committed so normal builds need no image tooling.

To regenerate (development only):

```sh
python3 -m venv /tmp/smartstage-icon-tools
/tmp/smartstage-icon-tools/bin/python -m pip install CairoSVG==2.8.2 Pillow==12.1.1
/tmp/smartstage-icon-tools/bin/python scripts/generate-icons.py
```

CairoSVG also requires the platform's Cairo library. Windows ICO contains
16, 20, 24, 32, 40, 48, 64, 128 and 256 pixel images. Mac ICNS contains standard
and Retina representations through 1024 pixels.

The Windows build compiles `packaging/windows/smartstage.rc` into an
architecture-specific Go `.syso` resource before linking, then removes the
temporary object. The icon is inside the executable; no sidecar is needed.

Mac builds also create `smartstage-darwin-<arch>.app.zip`. The optional
`Smart Stage.app` includes the same standalone executable, a native launcher,
the ICNS icon and bundle metadata. Opening it in Finder starts Smart Stage in
Terminal so pairing keys, logs and Ctrl+C remain available. The persistent
custom icon belongs to the Finder app; the running terminal uses Terminal's
Dock icon. The curl installer continues to install the standalone CLI.

References: [Windows ICON resources](https://learn.microsoft.com/en-us/windows/win32/menurc/icon-resource),
[Apple bundle structure and icon metadata](https://developer.apple.com/library/archive/documentation/CoreFoundation/Conceptual/CFBundles/BundleTypes/BundleTypes.html).
