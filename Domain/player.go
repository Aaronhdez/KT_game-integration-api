package domain

// Player represents a user in the system with associated credentials and currency.
// PasswordHash must never be logged or returned.
type Player struct {
	UserID       UserID
	Username     string
	PasswordHash string
	Currency     Currency
}
