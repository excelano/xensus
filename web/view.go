package web

import (
	"net/http"

	"github.com/excelano/xensus/auth"
	"github.com/excelano/xensus/config"
)

// BaseView carries the data every page needs: the <title>, the signed-in user
// (for nav identity and steward-only controls), and which nav links to show.
// Every page view embeds it so layout.html renders the chrome uniformly. It is
// exported so html/template can reach the fields promoted from the embed.
type BaseView struct {
	Title string
	User  *auth.User
	Nav   navView
}

// navView records which nav links the current user should see. A surface link
// shows when reads of that surface are open, or when the user is a steward
// (who can read every surface). This keeps a non-steward from seeing links to
// surfaces that would only 403 them once locked via XENSUS_STEWARD_ONLY.
type navView struct {
	ShowPersons  bool
	ShowSystems  bool
	ShowStewards bool
	ShowAudit    bool
}

// base builds the shared view data for a page. The user is read from request
// context; it is nil for an anonymous request, though the page gates redirect
// those to sign-in before any render happens.
func (h *Handlers) base(r *http.Request, title string) BaseView {
	u, _ := auth.UserFrom(r.Context())
	steward := u != nil && u.IsSteward
	canRead := func(surface string) bool {
		return steward || !h.access.StewardOnly(surface)
	}
	return BaseView{
		Title: title,
		User:  u,
		Nav: navView{
			ShowPersons:  canRead(config.SurfacePersons),
			ShowSystems:  canRead(config.SurfaceSystems),
			ShowStewards: canRead(config.SurfaceStewards),
			ShowAudit:    canRead(config.SurfaceAudit),
		},
	}
}
