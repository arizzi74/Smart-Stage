#!/bin/sh
# Install the verified Finder bundle. Gateway mode needs no firewall changes.
# Keep execution inside main so a truncated curl download cannot start an install.
set -eu

fail() {
    printf 'Smart Stage: %s\n' "$*" >&2
    exit 1
}

bundle_id() {
    /usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$1/Contents/Info.plist" 2>/dev/null
}

check_destination() {
    [ ! -L "$destination" ] || fail "Refusing to replace a symbolic link: $destination"
    if [ -e "$destination" ]; then
        [ -d "$destination" ] || fail "The installation path is not an app directory: $destination"
        existing_id=$(bundle_id "$destination") || fail "The existing app has no readable bundle identifier: $destination"
        [ "$existing_id" = 'com.github.arizzi74.smartstage' ] || fail "The existing app is not Smart Stage: $destination"
        if [ -f "$destination/Contents/MacOS/smartstage" ]; then
            running=$(/usr/sbin/lsof -t "$destination/Contents/MacOS/smartstage" 2>/dev/null || :)
            [ -z "$running" ] || fail 'Smart Stage is running. Quit it from its Dock or menu bar entry (or Ctrl+C for a Terminal launch), then run the installer again.'
        fi
    fi
    if [ "$no_launch" = 0 ]; then
        if [ "$lan_firewall" = 1 ]; then
            listeners=$(/usr/sbin/lsof -nP -iTCP:8787 -iTCP:8788 -sTCP:LISTEN -t 2>/dev/null || :)
        else
            listeners=$(/usr/sbin/lsof -nP -iTCP:8787 -sTCP:LISTEN -t 2>/dev/null || :)
        fi
        [ -z "$listeners" ] || fail 'A Smart Stage port is already in use. Close the existing Smart Stage session, or free its ports, then run the installer again.'
    fi
}

clear_quarantine() {
    # The bundle has already been checked for symlinks. Remove this attribute
    # alone, and only where present; retain every other extended attribute.
    /usr/bin/find "$1" -exec /bin/sh -c '
        for item do
            if /usr/bin/xattr -p com.apple.quarantine "$item" >/dev/null 2>&1; then
                /usr/bin/xattr -d com.apple.quarantine "$item" || exit 1
            fi
        done
    ' sh {} +
}

firewall_app_state() {
    state=$(LC_ALL=C /usr/libexec/ApplicationFirewall/socketfilterfw --getappblocked "$1" 2>/dev/null || :)
    printf '%s\n' "$state" | /usr/bin/awk '
        / is not blocked[.]?[[:space:]]*$/ || / is permitted[.]?[[:space:]]*$/ || /^[[:space:]]*\([[:space:]]*Allow incoming connections[[:space:]]*\)[[:space:]]*$/ { allowed = 1 }
        / is blocked[.]?[[:space:]]*$/ || /^[[:space:]]*\([[:space:]]*Block incoming connections[[:space:]]*\)[[:space:]]*$/ { blocked = 1 }
        END { if (blocked) print "blocked"; else if (allowed) print "allowed"; else print "unknown" }
    '
}

configure_firewall() {
    if [ "$skip_firewall" = 1 ]; then
        printf 'Firewall setup skipped (SMARTSTAGE_SKIP_FIREWALL=1).\n'
        return 0
    fi
    if [ "$lan_firewall" != 1 ]; then
        printf 'Public gateway mode needs no incoming-connection firewall rule. Choose Local network in Admin to enable LAN remote control.\n'
        return 0
    fi
    firewall_core="$destination/Contents/MacOS/smartstage"
    if [ ! -x /usr/libexec/ApplicationFirewall/socketfilterfw ]; then
        printf 'Smart Stage is installed, but the macOS firewall tool is unavailable.\n' >&2
        return 1
    fi
    core_state=$(firewall_app_state "$firewall_core")
    bundle_state=$(firewall_app_state "$destination")
    installed_core_sha=$(/usr/bin/shasum -a 256 "$firewall_core")
    installed_core_sha=${installed_core_sha%% *}
    # An allowed path for older ad-hoc signed bytes is not sufficient evidence
    # for this release. Refresh the core rule whenever the installed bytes change.
    if [ "$previous_core_sha" = "$installed_core_sha" ] && [ "$core_state" = allowed ] && [ "$bundle_state" != blocked ]; then
        printf 'The installed Smart Stage executable is already allowed by the macOS firewall.\n'
        return 0
    fi
    unblock_bundle=0
    if [ "$bundle_state" = blocked ]; then unblock_bundle=1; fi
    printf 'macOS will request administrator approval to allow incoming connections to this Smart Stage executable.\n'
    printf 'Target: %s\n' "$firewall_core"
    # Pass paths as argv, then use AppleScript's shell quoting. Never interpolate
    # user-controlled paths into an elevated shell command or executable script.
    if ! /usr/bin/osascript - "$firewall_core" "$destination" "$unblock_bundle" <<'SMARTSTAGE_FIREWALL_APPLESCRIPT'
on run arguments
    set firewallTool to "/usr/libexec/ApplicationFirewall/socketfilterfw"
    set corePath to quoted form of (item 1 of arguments)
    set command to "LC_ALL=C " & firewallTool & " --add " & corePath & " && LC_ALL=C " & firewallTool & " --unblockapp " & corePath
    if item 3 of arguments is "1" then
        set bundlePath to quoted form of (item 2 of arguments)
        set command to command & " && LC_ALL=C " & firewallTool & " --unblockapp " & bundlePath
    end if
    do shell script command with administrator privileges
end run
SMARTSTAGE_FIREWALL_APPLESCRIPT
    then
        printf 'Smart Stage is installed, but firewall approval was cancelled or denied.\n' >&2
        printf 'Open System Settings > Network > Firewall > Options and allow incoming connections for Smart Stage, or rerun this installer. A managed Mac may require your administrator.\n' >&2
        return 1
    fi
    core_state=$(firewall_app_state "$firewall_core")
    bundle_state=$(firewall_app_state "$destination")
    if [ "$core_state" != allowed ] || [ "$bundle_state" = blocked ]; then
        printf 'Smart Stage is installed, but its firewall allowance could not be verified.\n' >&2
        LC_ALL=C /usr/libexec/ApplicationFirewall/socketfilterfw --getappblocked "$firewall_core" >&2 || :
        LC_ALL=C /usr/libexec/ApplicationFirewall/socketfilterfw --getappblocked "$destination" >&2 || :
        printf 'Check System Settings > Network > Firewall > Options. A managed firewall policy may require your administrator.\n' >&2
        return 1
    fi
    printf 'Verified: the installed Smart Stage executable has an allow rule in the macOS app firewall.\n'
}

cleanup() {
    result=$?
    trap - 0 HUP INT TERM
    preserve_work=0
    if [ "$complete" -ne 1 ]; then
        if [ "$installed_new" -eq 1 ]; then
            if [ -e "$destination" ] || [ -L "$destination" ]; then
                current_identity=$(/usr/bin/stat -f '%d:%i' "$destination" 2>/dev/null || :)
                if [ -d "$destination" ] && [ ! -L "$destination" ] && [ "$current_identity" = "$staged_identity" ]; then
                    /bin/rm -rf "$destination" || preserve_work=1
                else
                    preserve_work=1
                fi
            fi
        fi
        # Check the backup itself: a signal may arrive after rename succeeds
        # but before the shell could record its success in a variable.
        if [ -n "$work" ] && [ -d "$work/previous.app" ] && [ ! -L "$work/previous.app" ]; then
            if [ ! -e "$destination" ] && [ ! -L "$destination" ]; then
                /bin/mv "$work/previous.app" "$destination" || preserve_work=1
            else
                preserve_work=1
            fi
            if [ "$preserve_work" -eq 1 ]; then
                printf 'Smart Stage: previous app retained for recovery at %s/previous.app\n' "$work" >&2
            fi
        fi
    fi
    if [ -n "$work" ] && [ "$preserve_work" -eq 0 ]; then
        /bin/rm -rf "$work"
    fi
    if [ "$lock_owned" -eq 1 ]; then
        /bin/rm -f "$lock/pid"
        /bin/rmdir "$lock" || :
    fi
    exit "$result"
}

main() {
    [ "$(/usr/bin/uname -s)" = Darwin ] || fail 'This installer is for macOS. Use a Windows ZIP from the GitHub release on Windows.'
    version=${SMARTSTAGE_VERSION:-v1.1.1}
    case "$version" in
        v[0-9]*) ;;
        *) fail 'SMARTSTAGE_VERSION must be a release tag such as v1.0.0.' ;;
    esac
    case "$version" in
        *[!A-Za-z0-9._-]*) fail 'SMARTSTAGE_VERSION contains an invalid character.' ;;
    esac
    no_launch=${SMARTSTAGE_NO_LAUNCH:-0}
    case "$no_launch" in 0|1) ;; *) fail 'SMARTSTAGE_NO_LAUNCH must be 0 or 1.' ;; esac
    skip_firewall=${SMARTSTAGE_SKIP_FIREWALL:-0}
    case "$skip_firewall" in 0|1) ;; *) fail 'SMARTSTAGE_SKIP_FIREWALL must be 0 or 1.' ;; esac
    lan_firewall=${SMARTSTAGE_CONFIGURE_LAN_FIREWALL:-0}
    case "$lan_firewall" in 0|1) ;; *) fail 'SMARTSTAGE_CONFIGURE_LAN_FIREWALL must be 0 or 1.' ;; esac
    # Releases before the gateway shipped always run a LAN listener. Retain
    # their original firewall behavior during the release transition and when
    # an operator deliberately installs an older version. SKIP still wins.
    case "$version" in
        v0.1.0-preview.[0-9]|v0.1.0-preview.1[0-4]) lan_firewall=1 ;;
    esac
    gateway_settings="$HOME/Library/Application Support/SmartStage/gateway.json"
    if [ -f "$gateway_settings" ] && [ ! -L "$gateway_settings" ]; then
        saved_mode=$(/usr/bin/plutil -extract mode raw -o - "$gateway_settings" 2>/dev/null || :)
        if [ "$saved_mode" = lan ]; then lan_firewall=1; fi
    fi
    case "$(/usr/bin/uname -m)" in
        arm64) arch=arm64 ;;
        x86_64)
            translated=$(/usr/sbin/sysctl -in sysctl.proc_translated 2>/dev/null || :)
            arm_capable=$(/usr/sbin/sysctl -in hw.optional.arm64 2>/dev/null || :)
            if [ "$translated" = 1 ] || [ "$arm_capable" = 1 ]; then arch=arm64; else arch=amd64; fi
            ;;
        *) fail 'This Mac architecture is not supported.' ;;
    esac

    install_parent=${SMARTSTAGE_INSTALL_DIR:-"$HOME/Applications"}
    case "$install_parent" in
        /*) ;;
        *) fail 'SMARTSTAGE_INSTALL_DIR must be an absolute parent directory.' ;;
    esac
    /bin/mkdir -p "$install_parent"
    install_parent=$(CDPATH= cd -P "$install_parent" && pwd -P)
    destination="$install_parent/Smart Stage.app"
    lock="$install_parent/.smartstage-install.lock"
    lock_owned=0
    work=''
    installed_new=0
    staged_identity=''
    complete=0
    trap cleanup 0
    trap 'exit 129' HUP
    trap 'exit 130' INT
    trap 'exit 143' TERM
    umask 077
    if ! /bin/mkdir "$lock" 2>/dev/null; then
        fail "Another installation may be in progress (lock: $lock). If an earlier installer was interrupted, remove that lock directory after confirming it is no longer running."
    fi
    lock_owned=1
    printf '%s\n' "$$" > "$lock/pid"
    check_destination
    work=$(/usr/bin/mktemp -d "$install_parent/.smartstage-install.XXXXXXXX")

    artifact="smartstage-darwin-$arch.app.zip"
    archive="$work/$artifact"
    checksum="$archive.sha256"
    base="https://github.com/arizzi74/Smart-Stage/releases/download/$version"
    printf 'Downloading Smart Stage %s for Mac %s…\n' "$version" "$arch"
    for name in "$artifact" "$artifact.sha256"; do
        /usr/bin/curl --fail --location --silent --show-error \
            --proto '=https' --proto-redir '=https' --tlsv1.2 \
            --retry 3 --connect-timeout 15 --max-time 300 \
            --output "$work/$name" "$base/$name"
    done
    /usr/bin/awk -v name="$artifact" '
        NF != 2 || length($1) != 64 || $1 ~ /[^0-9a-fA-F]/ || $2 != name { bad = 1 }
        END { exit (bad || NR != 1) }
    ' "$checksum" || fail 'The release checksum file is invalid.'
    expected=$(/usr/bin/awk '{ print tolower($1) }' "$checksum")
    actual=$(/usr/bin/shasum -a 256 "$archive")
    actual=${actual%% *}
    [ "$actual" = "$expected" ] || fail 'The download checksum does not match. Nothing was installed.'

    # Inspect paths and Unix file types before extraction, including entries
    # that could otherwise create a symlink and write through it during unzip.
    /usr/bin/unzip -Z -1 "$archive" > "$work/archive-paths"
    /usr/bin/awk '
        {
            if ($0 !~ /^Smart Stage[.]app\// && $0 !~ /^__MACOSX\//) bad = 1
            if ($0 ~ /\\/) bad = 1
            n = split($0, parts, "/")
            for (i = 1; i <= n; i++) {
                if (parts[i] == "." || parts[i] == ".." || (parts[i] == "" && i != n)) bad = 1
            }
        }
        END { exit (bad || NR == 0) }
    ' "$work/archive-paths" || fail 'The release archive contains an unsafe path.'
    /usr/bin/unzip -Z -l "$archive" > "$work/archive-modes"
    /usr/bin/awk '
        length($1) == 10 && substr($1, 1, 1) ~ /[bclps]/ { bad = 1 }
        END { exit bad }
    ' "$work/archive-modes" || fail 'The release archive contains links or special files.'
    /bin/mkdir "$work/extracted"
    /usr/bin/ditto -x -k "$archive" "$work/extracted"
    staged="$work/extracted/Smart Stage.app"
    [ -d "$staged" ] && [ ! -L "$staged" ] || fail 'The archive does not contain Smart Stage.app.'
    unsafe=$(/usr/bin/find "$staged" ! -type f ! -type d -print)
    [ -z "$unsafe" ] || fail 'The extracted app contains links or special files.'
    [ "$(bundle_id "$staged")" = 'com.github.arizzi74.smartstage' ] || fail 'The downloaded bundle is not Smart Stage.'
    [ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleExecutable' "$staged/Contents/Info.plist")" = SmartStageLauncher ] || fail 'The downloaded app has an unexpected launcher.'
    [ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIconFile' "$staged/Contents/Info.plist")" = smartstage.icns ] || fail 'The downloaded app has an unexpected icon.'
    [ "$(/usr/libexec/PlistBuddy -c 'Print :SmartStageVersion' "$staged/Contents/Info.plist")" = "$version" ] || fail 'The downloaded bundle has an unexpected version.'
    background_launch=$(/usr/libexec/PlistBuddy -c 'Print :SmartStageBackgroundLaunch' "$staged/Contents/Info.plist" 2>/dev/null || :)
    dock_icon=$(/usr/libexec/PlistBuddy -c 'Print :SmartStageDockIcon' "$staged/Contents/Info.plist" 2>/dev/null || :)
    native_admin_window=$(/usr/libexec/PlistBuddy -c 'Print :SmartStageNativeAdminWindow' "$staged/Contents/Info.plist" 2>/dev/null || :)
    for executable in "$staged/Contents/MacOS/smartstage" "$staged/Contents/MacOS/SmartStageLauncher" "$staged/Contents/Resources/Start Smart Stage.command"; do
        [ -f "$executable" ] && [ -x "$executable" ] || fail 'The downloaded app is missing an executable.'
    done
    [ -s "$staged/Contents/Resources/smartstage.icns" ] || fail 'The downloaded app is missing its icon.'
    /usr/bin/codesign --verify --deep --strict "$staged" || fail 'The downloaded app failed its signature integrity check.'

    # Stage on the destination volume so these renames do not copy a half app.
    # Recheck immediately before replacement; never stop a running show.
    check_destination
    previous_core_sha=''
    if [ -e "$destination" ]; then
        if [ -f "$destination/Contents/MacOS/smartstage" ]; then
            previous_core_sha=$(/usr/bin/shasum -a 256 "$destination/Contents/MacOS/smartstage")
            previous_core_sha=${previous_core_sha%% *}
        fi
        /bin/mv "$destination" "$work/previous.app"
    fi
    staged_identity=$(/usr/bin/stat -f '%d:%i' "$staged")
    # Record intent before rename. Cleanup identifies our exact directory by
    # device/inode, so interrupted or failed renames cannot remove another app.
    installed_new=1
    /bin/mv "$staged" "$destination"
    printf 'Removing the Gatekeeper quarantine attribute only from this verified Smart Stage app.\n'
    clear_quarantine "$destination" || fail 'Could not remove the app quarantine attribute.'
    installed_version=$("$destination/Contents/MacOS/smartstage" --version) || fail 'The installed executable could not start.'
    printf '%s\n' "$installed_version" | /usr/bin/awk -v version="$version" -v arch="$arch" '
        $1 == "Smart" && $2 == "Stage" && $3 == version && $NF == "darwin/" arch { valid = 1 }
        END { exit (!valid || NR != 1) }
    ' || fail 'The installed executable reports an unexpected version or architecture.'
    # Installation is now complete. Cancelling the optional system permission
    # prompt must leave these verified files installed, rather than roll back.
    complete=1
    printf 'Installed %s\n%s\n' "$destination" "$installed_version"
    post_install_status=0
    configure_firewall || post_install_status=1
    if [ "$no_launch" = 0 ]; then
        if ! /usr/bin/open "$destination"; then
            printf 'Smart Stage is installed, but macOS could not open it. Open Smart Stage.app manually.\n' >&2
            post_install_status=1
        fi
        if [ "$dock_icon" = true ]; then
            if [ "$native_admin_window" = true ]; then
                printf 'Smart Stage opens its own Admin window. Drop Finder files into the window to add them without copying. Use Quit Smart Stage to stop playback and exit.\n'
            else
                printf 'Smart Stage appears in the Dock and opens Admin in your browser. Right-click its Dock icon and choose Quit to close it.\n'
            fi
            printf 'Log: %s/Library/Logs/Smart Stage/smartstage.log\n' "$HOME"
        elif [ "$background_launch" = true ]; then
            printf 'Smart Stage runs from its menu bar icon and opens Admin in your browser. Use the icon to reopen Admin, view the log, or quit.\n'
            printf 'Log: %s/Library/Logs/Smart Stage/smartstage.log\n' "$HOME"
        else
            printf 'Smart Stage opens in Terminal and opens Admin in your browser. Press Ctrl+C in its Terminal window to stop it.\n'
        fi
    else
        printf 'Open Smart Stage.app to start it.\n'
    fi
    return "$post_install_status"
}

main "$@"
