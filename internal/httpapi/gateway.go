package httpapi

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"

	"smartstage/internal/app"
	"smartstage/internal/auth"
	"smartstage/internal/gateway"
	"smartstage/internal/remote"
)

// NewGatewayCommand is only passed to the authenticated outbound tunnel. It
// has no Admin routes, no LAN session authority and endpoint-scoped cookies.
func NewGatewayCommand(service *app.Service, authentication *auth.Manager, assets http.Handler, publicURL, prefix string) (*API, error) {
	u, err := gateway.ValidateURL(publicURL)
	if err != nil {
		return nil, err
	}
	if !regexp.MustCompile(`^/smartstage/e/[a-f0-9]{32}/$`).MatchString(prefix) {
		return nil, errors.New("invalid public endpoint prefix")
	}
	port := 443
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return nil, errors.New("invalid gateway port")
		}
	}
	a := NewCommand(service, authentication, assets, []string{u.Hostname()}, port)
	a.cookiePath = prefix
	a.requireTLS = true
	return a, nil
}

type GatewayController interface {
	Status() remote.Status
	Configure(remote.Edit) (remote.Status, error)
	Reconnect() (remote.Status, error)
}

func (a *API) SetGateway(controller GatewayController) {
	a.mu.Lock()
	a.gateway = controller
	a.mu.Unlock()
}
func (a *API) gatewayRequest(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	controller := a.gateway
	a.mu.RUnlock()
	if controller == nil {
		fail(w, 503, "gateway_unavailable", "Gateway settings are unavailable")
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, 200, controller.Status())
		return
	}
	if a.app.Snapshot(true).UpdatePending {
		fail(w, 409, "updating", "Wait for Smart Stage to finish updating or restarting")
		return
	}
	var status remote.Status
	var err error
	if r.URL.Path == "/api/gateway/reconnect" {
		var body *struct{}
		if !decode(w, r, &body) {
			return
		}
		if body == nil {
			fail(w, 400, "invalid_json", "Send an empty JSON object")
			return
		}
		status, err = controller.Reconnect()
	} else {
		var edit remote.Edit
		if !decode(w, r, &edit) {
			return
		}
		status, err = controller.Configure(edit)
	}
	if err != nil {
		var appError *app.Error
		if errors.As(err, &appError) {
			respondError(w, err)
		} else {
			fail(w, 400, "gateway_settings", err.Error())
		}
		return
	}
	writeJSON(w, 200, status)
	if status.Restart {
		_ = http.NewResponseController(w).Flush()
	}
}
