package server

import (
	"context"
	"fmt"

	"github.com/Sake-My/Komari-Nova/database/auditlog"
	"github.com/Sake-My/Komari-Nova/utils/geoip"
	logger "github.com/Sake-My/Komari-Nova/utils/log"
	"github.com/Sake-My/Komari-Nova/utils/messageSender"
	"github.com/Sake-My/Komari-Nova/web/oauth"
)

// InitProviders initializes providers needed by the normal application.
func (a *App) InitProviders() error {
	a.initOAuth()

	go geoip.InitGeoIp()
	a.addCleanup("geoip", func(context.Context) error { return geoip.Shutdown() })

	messageSender.Initialize()
	a.addCleanup("message-sender", func(context.Context) error { return messageSender.Shutdown() })
	return nil
}

// initOAuth initializes OAuth once. Restricted authenticated guides need it
// before the normal provider initialization phase.
func (a *App) initOAuth() {
	if a.oauthReady {
		return
	}
	if err := oauth.Initialize(); err != nil {
		logger.Errorf("server", "Failed to initialize OAuth provider: %v", err)
		auditlog.EventLog("error", fmt.Sprintf("Failed to initialize OAuth provider: %v", err))
	}
	a.oauthReady = true
	a.addCleanup("oauth", func(context.Context) error { return oauth.Shutdown() })
}
