// Package rates prices energy: one flat rate, or the price of each electricity bill for the dates it covers.
// It is a port of KVGuzzler.Core.Billing (Bill, BillValidator, RateSchedule), with the same rules and the same tests.
package rates

import (
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

// Money is an exact amount (or an exact price per kWh), never a float. The zero value is 0.
type Money struct{ r *big.Rat }

var moneyPattern = regexp.MustCompile(`^-?\d+(\.\d{1,6})?$`)

// ParseMoney reads a plain decimal such as "12.34" or "-4", with up to six decimals.
func ParseMoney(s string) (Money, error) {
	if !moneyPattern.MatchString(s) {
		return Money{}, fmt.Errorf("%q is not an amount: use digits with an optional decimal point, like 12.34", s)
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return Money{}, fmt.Errorf("%q is not an amount", s)
	}
	return Money{r}, nil
}

// MustMoney is ParseMoney for constants in code and tests.
func MustMoney(s string) Money {
	m, err := ParseMoney(s)
	if err != nil {
		panic(err)
	}
	return m
}

// MoneyFromFloat is the exact value of f (a float is binary, so 0.1 is not exactly one tenth; use it for energy, not prices).
func MoneyFromFloat(f float64) Money {
	r := new(big.Rat)
	if r.SetFloat64(f) == nil {
		return Money{}
	}
	return Money{r}
}

func (m Money) rat() *big.Rat {
	if m.r == nil {
		return new(big.Rat)
	}
	return m.r
}

func (m Money) Add(o Money) Money        { return Money{new(big.Rat).Add(m.rat(), o.rat())} }
func (m Money) Sub(o Money) Money        { return Money{new(big.Rat).Sub(m.rat(), o.rat())} }
func (m Money) Mul(o Money) Money        { return Money{new(big.Rat).Mul(m.rat(), o.rat())} }
func (m Money) Cmp(o Money) int          { return m.rat().Cmp(o.rat()) }
func (m Money) Equal(o Money) bool       { return m.Cmp(o) == 0 }
func (m Money) Sign() int                { return m.rat().Sign() }
func (m Money) Float() float64           { f, _ := m.rat().Float64(); return f }
func (m Money) Div(o Money) Money        { return Money{new(big.Rat).Quo(m.rat(), o.rat())} }
func (m Money) MulFloat(f float64) Money { return m.Mul(MoneyFromFloat(f)) }

// String is the amount rounded to six decimals, keeping at least two ("12.30", "0.199155").
func (m Money) String() string {
	s := m.rat().FloatString(6)
	if i := strings.IndexByte(s, '.'); i >= 0 {
		s = strings.TrimRight(s, "0")
		if len(s)-i-1 < 2 {
			s += strings.Repeat("0", 2-(len(s)-i-1))
		}
	}
	return s
}

// Fixed is the amount rounded to the given number of decimals, always with that many ("0.0020").
func (m Money) Fixed(places int) string { return m.rat().FloatString(places) }

func (m Money) MarshalJSON() ([]byte, error) { return json.Marshal(m.String()) }

func (m *Money) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("an amount is a decimal string like \"12.34\"")
	}
	v, err := ParseMoney(s)
	if err != nil {
		return err
	}
	*m = v
	return nil
}
