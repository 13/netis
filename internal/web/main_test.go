package web

import (
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// shippedBcryptCost is bcryptCost as the package initialises it, captured
// before TestMain lowers it.
var shippedBcryptCost = bcryptCost

// TestMain hashes passwords at bcrypt's minimum cost. At the default every
// login, setup and user created in a test spends tens of milliseconds, several
// times that under the race detector, and the suite took minutes.
func TestMain(m *testing.M) {
	bcryptCost = bcrypt.MinCost
	os.Exit(m.Run())
}

// The lowered cost is a test convenience only; what ships must stay at the
// library default.
func TestShippedBcryptCostIsDefault(t *testing.T) {
	if shippedBcryptCost != bcrypt.DefaultCost {
		t.Errorf("bcryptCost = %d, want bcrypt.DefaultCost (%d)", shippedBcryptCost, bcrypt.DefaultCost)
	}
}
