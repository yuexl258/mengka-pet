package config

type Config struct {
	Addr           string
	Version        string
	Environment    string
	DatabasePath   string
	FrontendOrigin string
	CookieName     string
	SessionHours   int
	CookieSecure   bool
	LoginLimit     int
	LoginWindowSec int
}

func Load() Config {
	return Config{
		Addr:           ":8080",
		Version:        "0.2.6",
		Environment:    "development",
		DatabasePath:   "./data/qq-pet.db",
		FrontendOrigin: "http://localhost:5173",
		CookieName:     "qq_pet_session",
		SessionHours:   168,
		CookieSecure:   false,
		LoginLimit:     10,
		LoginWindowSec: 300,
	}
}
