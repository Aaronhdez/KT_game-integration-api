-- +goose Up
-- The four players of the Wallet mock (docs/wallet-findings.md, Q10): user IDs
-- and currencies must match the Wallet's. Passwords are documented in README.md;
-- only their bcrypt hashes (cost 10) are stored.
INSERT INTO players (wallet_user_id, username, password_hash, currency) VALUES
    (34633089486, 'player_usd',   '$2a$10$8YD78fz6fF2LgDPYhVqyeONIEfRCvIekJHrDUVMXBcbCAUl8osmhW', 'USD'),
    (34679664254, 'player_eur',   '$2a$10$zXEmEEWpCv/WCz5NzOob5eVV6xWP3sr1zyxhpQg83WxV3HmJnUiBy', 'EUR'),
    (34616761765, 'player_kes',   '$2a$10$WCiBay5VsExFxqQKNiJvwuiM3McbNdQJNkgESyTAKDE41wrYY8YhK', 'KES'),
    (34673635133, 'player_usd_2', '$2a$10$m7AM1mNMmUIN/e44UWEVaupCB7enYS0bxrfQZpnXBAxWciTXYapv2', 'USD');

-- +goose Down
DELETE FROM players
WHERE wallet_user_id IN (34633089486, 34679664254, 34616761765, 34673635133);
