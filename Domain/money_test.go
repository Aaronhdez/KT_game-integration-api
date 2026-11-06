package domain

import (
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"
)

type amountPayload struct {
	Amount Money `json:"amount"`
}

func TestMoneyFromDecimal_ValidAmount_IsAccepted(t *testing.T) {
	testCases := []struct {
		name  string
		value decimal.Decimal
	}{
		{name: "zero amount", value: decimal.NewFromInt(0)},
		{name: "valid amount", value: decimal.NewFromInt(1)},
		{name: "valid amount with decimal", value: decimal.RequireFromString("1.12")},
		{name: "maximum valid amount", value: decimal.RequireFromString("999999999999999999.99")},
		{name: "one cent", value: decimal.RequireFromString("0.01")},
		{name: "trailing zero beyond two decimals", value: decimal.RequireFromString("1.100")},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := MoneyFromDecimal(testCase.value)

			assertNoError(t, err)
		})
	}
}

func TestMoneyFromDecimal_InvalidAmount_ReturnsInvalidAmount(t *testing.T) {
	testCases := []struct {
		name  string
		value decimal.Decimal
	}{
		{name: "negative amount", value: decimal.NewFromInt(-1)},
		{name: "exceeds upper limit", value: decimal.RequireFromString("1000000000000000000")},
		{name: "too many decimal places", value: decimal.RequireFromString("1.123")},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := MoneyFromDecimal(testCase.value)

			assertErrorIs(t, err, ErrInvalidAmount)
		})
	}
}

func TestParseMoney_ValidText_IsAccepted(t *testing.T) {
	testCases := []struct {
		name  string
		input string
	}{
		{name: "integer", input: "5000"},
		{name: "two decimals", input: "4989.50"},
		{name: "largest seeded balance", input: "9000000000.09"},
		{name: "exponent notation", input: "1e3"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := ParseMoney(testCase.input)

			assertNoError(t, err)
		})
	}
}

func TestParseMoney_InvalidText_ReturnsInvalidAmount(t *testing.T) {
	testCases := []struct {
		name  string
		input string
	}{
		{name: "too many decimal places", input: "1.123"},
		{name: "NAN", input: "NaN"},
		{name: "infinity", input: "Inf"},
		{name: "decimal comma", input: "10,12"},
		{name: "non-numeric", input: "abc"},
		{name: "surrounding spaces", input: " 10 "},
		{name: "empty string", input: ""},
		{name: "only decimal point", input: "."},
		{name: "only decimal point with whitespace", input: " . "},
		{name: "only whitespace", input: "   "},
		{name: "only plus sign", input: "+"},
		{name: "only minus sign", input: "-"},
		{name: "negative amount", input: "-1"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := ParseMoney(testCase.input)

			assertErrorIs(t, err, ErrInvalidAmount)
		})
	}
}

func TestMoney_ZeroAmount_IsZero(t *testing.T) {
	var zeroMoney = givenAParsedMoney(t, "0")

	assertTrue(t, zeroMoney.IsZero(), "expected zeroMoney to be zero")
}

func TestMoney_ZeroValue_IsZeroWithTwoDecimals(t *testing.T) {
	var result Money

	assertStringEquals(t, result.String(), "0.00", "zero value of Money should print 0.00")
}

func TestMoney_OneUnit_IsPositive(t *testing.T) {
	var positiveMoney = givenAParsedMoney(t, "1")

	assertTrue(t, positiveMoney.IsPositive(), "expected positiveMoney to be positive")
}

func TestMoney_OneCent_IsPositive(t *testing.T) {
	var oneCentMoney = givenAParsedMoney(t, "0.01")

	assertTrue(t, oneCentMoney.IsPositive(), "expected oneCentMoney to be positive")
}

func TestMoney_Zero_IsNotPositive(t *testing.T) {
	var zeroMoney = givenAParsedMoney(t, "0")

	assertFalse(t, zeroMoney.IsPositive(), "expected zeroMoney to not be positive")
}

func TestMoney_DifferentValues_AreNotEqual(t *testing.T) {
	var money1 = givenAParsedMoney(t, "1.0")
	var money2 = givenAParsedMoney(t, "2.0")

	assertFalse(t, money1.Equal(money2), "expected money1 and money2 to not be equal")
}

func TestMoney_StringWithOneDecimal_PadsToTwoDecimals(t *testing.T) {
	var money = givenAParsedMoney(t, "1.0")

	assertStringEquals(t, money.String(), "1.00", "expected money string to be padded to two decimals")
}

func TestMoney_SameValues_DifferentScales_AreEqual(t *testing.T) {
	var money1 = givenAParsedMoney(t, "1.0")
	var money2 = givenAParsedMoney(t, "1.00")

	assertTrue(t, money1.Equal(money2), "expected money1 and money2 to be equal")
}

func TestMoney_MarshalJSON_WritesNumberWithTwoDecimals(t *testing.T) {
	var payload = amountPayload{Amount: givenAParsedMoney(t, "10.5")}
	result, err := json.Marshal(payload)

	assertNoError(t, err)
	assertStringEquals(t, string(result), `{"amount":10.50}`, "money should be written as a JSON number with two decimals")
}

func TestMoney_UnmarshalJSON_ReadsNumber(t *testing.T) {
	var result amountPayload

	err := json.Unmarshal([]byte(`{"amount":10.50}`), &result)

	assertNoError(t, err)
	assertStringEquals(t, result.Amount.String(), "10.50", "money should be read from a JSON number")
}

////////////////////////////////////////////////////////////////////////////

func givenAParsedMoney(t *testing.T, value string) Money {
	t.Helper()
	money, err := ParseMoney(value)
	if err != nil {
		t.Fatalf("invalid money literal %q in test: %v", value, err)
	}
	return money
}

func assertTrue(t *testing.T, condition bool, message string) {
	t.Helper()
	if !condition {
		t.Errorf("Assertion failed: %s", message)
	}
}

func assertFalse(t *testing.T, condition bool, message string) {
	t.Helper()
	if condition {
		t.Errorf("Assertion failed: %s", message)
	}
}

func assertStringEquals(t *testing.T, actual string, expected string, message string) {
	t.Helper()
	if actual != expected {
		t.Errorf("Assertion failed: %q, expected: %q, actual: %q", message, expected, actual)
	}
}
