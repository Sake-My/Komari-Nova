package oauth

import (
	_ "github.com/Sake-My/Komari-Nova/web/oauth/factory"
	_ "github.com/Sake-My/Komari-Nova/web/oauth/generic"
	_ "github.com/Sake-My/Komari-Nova/web/oauth/github"
	_ "github.com/Sake-My/Komari-Nova/web/oauth/qq"
)

func All() {
	//empty function to ensure all OIDC providers are registered
}
