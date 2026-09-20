package messageSender

import (
	_ "github.com/Sake-My/Komari-Nova/utils/messageSender/bark"
	_ "github.com/Sake-My/Komari-Nova/utils/messageSender/email"
	_ "github.com/Sake-My/Komari-Nova/utils/messageSender/empty"
	_ "github.com/Sake-My/Komari-Nova/utils/messageSender/javascript"
	_ "github.com/Sake-My/Komari-Nova/utils/messageSender/serverchan3"
	_ "github.com/Sake-My/Komari-Nova/utils/messageSender/serverchanturbo"
	_ "github.com/Sake-My/Komari-Nova/utils/messageSender/telegram"
	_ "github.com/Sake-My/Komari-Nova/utils/messageSender/webhook"
)

func All() {
}
