#!/bin/sh
# Read-only connection checks. Run on the Mac hosting Smart Stage:
#   sh scripts/diagnose-macos.sh 192.168.0.117
# Optional second/third arguments override Admin/Command ports. No token needed.
set -u

if [ "$(uname -s)" != Darwin ]; then
    printf '%s\n' 'Run this diagnostic on the Mac hosting Smart Stage.' >&2
    exit 1
fi
if [ "$#" -gt 3 ]; then
    printf '%s\n' 'Usage: diagnose-macos.sh [remote IPv4 address] [Admin port] [Command port]' >&2
    exit 1
fi
smartstage_remote_ip=${1:-}
smartstage_admin_port=${2:-8787}
smartstage_command_port=${3:-8788}
case "$smartstage_remote_ip" in
    *[!0-9.]*) printf '%s\n' 'Use only the IPv4 address from Admin, without http://, a port, or a token.' >&2; exit 1 ;;
esac
for smartstage_port in "$smartstage_admin_port" "$smartstage_command_port"; do
    case "$smartstage_port" in
        ''|*[!0-9]*) printf '%s\n' 'Ports must be numbers from 1 to 65535.' >&2; exit 1 ;;
    esac
    if [ "${#smartstage_port}" -gt 5 ] || [ "$smartstage_port" -lt 1 ] || [ "$smartstage_port" -gt 65535 ]; then
        printf '%s\n' 'Ports must be numbers from 1 to 65535.' >&2
        exit 1
    fi
done
if [ -n "$smartstage_remote_ip" ] && ! printf '%s\n' "$smartstage_remote_ip" | /usr/bin/awk -F. '
    NF != 4 { exit 1 }
    { for (i = 1; i <= 4; i++) if ($i !~ /^[0-9]+$/ || length($i) > 3 || $i > 255) exit 1 }
'; then
    printf '%s\n' 'Use only the IPv4 address from Admin, without http://, a port, or a token.' >&2
    exit 1
fi

printf '%s\n' 'Smart Stage connection diagnostic (read-only; does not change firewall settings)'
/usr/bin/sw_vers -productVersion
/usr/bin/uname -m
printf '\n%s\n' 'Local IPv4 addresses:'
smartstage_addresses=$(/sbin/ifconfig | /usr/bin/awk '
    /^[^ \t]/ { interface = $1; sub(/:$/, "", interface) }
    $1 == "inet" && $2 !~ /^127\./ { print interface, $2 }
')
printf '%s\n' "${smartstage_addresses:-No non-loopback IPv4 address found.}"

printf '\n%s\n' 'Listening processes (Admin should be 127.0.0.1; Command should be * or the LAN address):'
/usr/sbin/lsof -nP "-iTCP:$smartstage_admin_port" "-iTCP:$smartstage_command_port" -sTCP:LISTEN || :
printf '\n%s\n' 'macOS firewall status:'
smartstage_firewall=/usr/libexec/ApplicationFirewall/socketfilterfw
if [ -x "$smartstage_firewall" ]; then
    "$smartstage_firewall" --getglobalstate || :
    "$smartstage_firewall" --getblockall || :
    printf '\n%s\n' 'Firewall rules for the running Smart Stage executable:'
    smartstage_listener_pids=$(/usr/sbin/lsof -nP "-iTCP:$smartstage_command_port" -sTCP:LISTEN -t 2>/dev/null || :)
    for smartstage_listener_pid in $smartstage_listener_pids; do
        case "$smartstage_listener_pid" in ''|*[!0-9]*) continue ;; esac
        # lsof reads mapped executable paths from the kernel. Restrict the
        # selection to the shipped core names and require the exact path to
        # exist: never infer an executable from a truncated ps command line.
        smartstage_core_paths=$(/usr/sbin/lsof -nP -a -p "$smartstage_listener_pid" -d txt -Fn 2>/dev/null |
            /usr/bin/sed -n 's/^n//p' |
            while IFS= read -r smartstage_mapped_path; do
                case "$smartstage_mapped_path" in
                    */smartstage|*/smartstage-darwin-arm64|*/smartstage-darwin-amd64)
                        if [ -f "$smartstage_mapped_path" ] && [ -x "$smartstage_mapped_path" ]; then
                            printf '%s\n' "$smartstage_mapped_path"
                        fi
                        ;;
                esac
            done)
        smartstage_core_count=$(printf '%s\n' "$smartstage_core_paths" | /usr/bin/awk 'NF { count++ } END { print count+0 }')
        if [ "$smartstage_core_count" -ne 1 ]; then
            printf 'PID %s: cannot identify one exact Smart Stage executable path; rule lookup skipped.\n' "$smartstage_listener_pid"
            continue
        fi
        printf 'PID %s executable: %s\n' "$smartstage_listener_pid" "$smartstage_core_paths"
        "$smartstage_firewall" --getappblocked "$smartstage_core_paths" || :
        case "$smartstage_core_paths" in
            *.app/Contents/MacOS/smartstage)
                smartstage_bundle_path=${smartstage_core_paths%/Contents/MacOS/smartstage}
                printf 'Containing app: %s\n' "$smartstage_bundle_path"
                "$smartstage_firewall" --getappblocked "$smartstage_bundle_path" || :
                ;;
        esac
    done
    if [ -z "$smartstage_listener_pids" ]; then
        printf '%s\n' 'No visible process is listening on the Command port.'
    fi
fi

smartstage_test_url() {
    printf '\nTesting %s\n' "$1"
    if [ "$#" -gt 1 ]; then
        printf 'With Host: %s\n' "$2"
        set -- "$1" --header "Host: $2"
    fi
    if /usr/bin/curl -q --noproxy '*' --connect-timeout 3 --max-time 5 \
        --verbose --output /dev/null \
        --write-out '\nHTTP status=%{http_code}; peer=%{remote_ip}; connect=%{time_connect}s; total=%{time_total}s\n' \
        "$@"; then
        printf '%s\n' 'curl exit=0 (check HTTP status above; expected 200)'
    else
        printf 'curl exit=%s\n' "$?"
    fi
}
smartstage_test_url "http://127.0.0.1:$smartstage_admin_port/admin"
smartstage_test_url "http://127.0.0.1:$smartstage_command_port/command"
if [ -n "$smartstage_remote_ip" ]; then
    if ! printf '%s\n' "$smartstage_addresses" | /usr/bin/awk -v wanted="$smartstage_remote_ip" '
        $2 == wanted { found = 1 }
        END { exit !found }
    '; then
        printf '\n%s\n' 'The supplied remote address is not currently assigned to this Mac. Copy the current link from Admin.'
    fi
    smartstage_test_url "http://$smartstage_remote_ip:$smartstage_command_port/command"
    printf '\n%s\n' 'Host-header swap: distinguish HTTP Host validation from the network destination.'
    smartstage_test_url "http://127.0.0.1:$smartstage_command_port/command" "$smartstage_remote_ip:$smartstage_command_port"
    smartstage_test_url "http://$smartstage_remote_ip:$smartstage_command_port/command" "127.0.0.1:$smartstage_command_port"
else
    printf '\n%s\n' 'To test the LAN URL too, rerun with its IPv4 address as the first argument (no token).'
fi
printf '\n%s\n' 'Copy this output when reporting the problem. Do not include your pairing token.'
