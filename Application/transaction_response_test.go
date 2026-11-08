package application

import (
	"net/http"
	"net/http/httptest"
	"testing"

	actions "github.com/aaronhdez/kt_game-integration-api/Actions"
	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// §7.2 status codes

func TestWriteTransactionResult_StatusCode(t *testing.T) {
	testCases := []struct {
		name   string
		result func(t *testing.T) actions.TransactionResult
		want   int
	}{
		{"completed for the first time", aCompletedResult, http.StatusCreated},
		{"replay of a completed one", aReplayedCompletedResult, http.StatusOK},
		{"pending for the first time", aPendingResult, http.StatusAccepted},
		{"replay of a pending one", aReplayedPendingResult, http.StatusAccepted},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()

			writeTransactionResult(recorder, testCase.result(t))

			assertStatusCode(t, recorder, testCase.want)
		})
	}
}

// §7.4 body

func TestWriteTransactionResult_Completed_WritesTheSection74Fields(t *testing.T) {
	recorder := httptest.NewRecorder()

	writeTransactionResult(recorder, aCompletedResult(t))

	assertBody(t, recorder, `{"transactionId":"b3f1c2d4-5e6f-4a7b-8c9d-0e1f2a3b4c5d","providerTransactionId":"prov-tx-0001","oldBalance":5000.00,"newBalance":4989.50,"status":"COMPLETED","betStatus":"PLACED"}`)
}

// §7.4: newBalance is null while PENDING; betStatus is null while a withdraw
// is PENDING, because the bet is not placed yet.
func TestWriteTransactionResult_Pending_WritesNullNewBalanceAndBetStatus(t *testing.T) {
	recorder := httptest.NewRecorder()

	writeTransactionResult(recorder, aPendingResult(t))

	assertBody(t, recorder, `{"transactionId":"b3f1c2d4-5e6f-4a7b-8c9d-0e1f2a3b4c5d","providerTransactionId":"prov-tx-0001","oldBalance":5000.00,"newBalance":null,"status":"PENDING","betStatus":null}`)
}

func TestWriteTransactionResult_SetsJSONContentType(t *testing.T) {
	recorder := httptest.NewRecorder()

	writeTransactionResult(recorder, aCompletedResult(t))

	assertContentTypeJSON(t, recorder)
}

// The body carries balances, which must never come from a cache.
func TestWriteTransactionResult_ForbidsCaching(t *testing.T) {
	recorder := httptest.NewRecorder()

	writeTransactionResult(recorder, aCompletedResult(t))

	assertHeader(t, recorder, "Cache-Control", "no-store")
}

/////////////////////////////////////////////////////////////////////////////////////

// The values of the §8.3 example.

func aPendingResult(t *testing.T) actions.TransactionResult {
	t.Helper()
	return actions.TransactionResult{
		Transaction: domain.Transaction{
			ID:                    "b3f1c2d4-5e6f-4a7b-8c9d-0e1f2a3b4c5d",
			Type:                  domain.TransactionWithdraw,
			Status:                domain.TransactionPending,
			UserID:                testUserID,
			BetID:                 1,
			ProviderTransactionID: "prov-tx-0001",
			Amount:                givenAParsedMoney(t, "10.50"),
			Currency:              domain.USD,
			OldBalance:            givenAParsedMoney(t, "5000.00"),
			CreatedAt:             testNow,
			UpdatedAt:             testNow,
		},
	}
}

func aReplayedPendingResult(t *testing.T) actions.TransactionResult {
	t.Helper()
	result := aPendingResult(t)
	result.Replayed = true
	return result
}

func aCompletedResult(t *testing.T) actions.TransactionResult {
	t.Helper()
	result := aPendingResult(t)
	newBalance := givenAParsedMoney(t, "4989.50")
	placed := domain.BetPlaced
	result.Transaction.Status = domain.TransactionCompleted
	result.Transaction.NewBalance = &newBalance
	result.BetStatus = &placed
	return result
}

func aReplayedCompletedResult(t *testing.T) actions.TransactionResult {
	t.Helper()
	result := aCompletedResult(t)
	result.Replayed = true
	return result
}
