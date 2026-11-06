package domain

// Player represents a user in the system with associated credentials and currency.
type Player struct {
	UserID       UserID
	Username     string
	PasswordHash string
	Currency     Currency
}
