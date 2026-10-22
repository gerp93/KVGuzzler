package rates

import (
	"errors"
	"fmt"
	"math"
	"unicode/utf8"
)

// FixedFee is a charge on a bill that does not depend on how much electricity was used (the basic service charge, a
// monthly credit). It is left out when working out the price per kWh. A credit is a negative amount.
type FixedFee struct {
	Label  string `json:"label"`
	Amount Money  `json:"amount"`
}

// Bill is one electricity bill: the dates it covers, what it totalled, the kWh it billed, and any flat fees to leave out.
// The price per kWh is what is left after those fees, divided by the kWh; with no fees it is the all-in price.
type Bill struct {
	Start     Date       `json:"start"` // first day covered (inclusive), a local calendar day
	End       Date       `json:"end"`   // last day covered (inclusive)
	Total     Money      `json:"total"`
	Kwh       float64    `json:"kwh"`
	FixedFees []FixedFee `json:"fixed_fees,omitempty"`
}

func (b Bill) FixedTotal() Money {
	var t Money
	for _, f := range b.FixedFees {
		t = t.Add(f.Amount)
	}
	return t
}

// Variable is what the bill charged for the electricity itself, after the flat fees are taken out.
func (b Bill) Variable() Money { return b.Total.Sub(b.FixedTotal()) }

func (b Bill) PricePerKwh() Money {
	if b.Kwh > 0 {
		return b.Variable().Div(MoneyFromFloat(b.Kwh))
	}
	return Money{}
}

func (b Bill) Days() int { return b.End.DaysSince(b.Start) + 1 }

// MaxBillDays is the longest a single bill can cover. Bills are about a month; this catches a year typed wrongly.
const MaxBillDays = 366

// Validate says why a bill cannot be saved, in words fit to show, or returns nil.
func (b Bill) Validate() error {
	switch {
	case b.End.Before(b.Start):
		return errors.New("The end date is before the start date.")
	case b.Days() > MaxBillDays:
		return errors.New("That covers more than a year. A bill usually covers about a month.")
	case !(b.Kwh > 0) || math.IsInf(b.Kwh, 0):
		return errors.New("Enter the kWh the bill charged for (more than 0).")
	case b.Total.Sign() <= 0:
		return errors.New("Enter the bill's total (more than 0).")
	}
	for _, f := range b.FixedFees {
		if utf8.RuneCountInString(f.Label) > 60 {
			return errors.New("A flat fee's name is too long (60 characters at most).")
		}
	}
	if b.Variable().Sign() <= 0 {
		return errors.New("After leaving out the flat fees nothing is left to price. Check the fees and the total.")
	}
	return nil
}

// ValidateAgainst is Validate plus the one rule between bills: two bills may not claim the same day (it would be priced
// twice). others are the other bills, not b itself. Past bills are welcome in any order.
func (b Bill) ValidateAgainst(others []Bill) error {
	if err := b.Validate(); err != nil {
		return err
	}
	for _, o := range others {
		if !o.Start.After(b.End) && !b.Start.After(o.End) {
			return fmt.Errorf("These dates overlap another bill (%s to %s). Each day can only be on one bill.", o.Start, o.End)
		}
	}
	return nil
}

// ValidateBills checks every bill and that no two overlap.
func ValidateBills(bills []Bill) error {
	for i, b := range bills {
		others := make([]Bill, 0, len(bills)-1)
		others = append(others, bills[:i]...)
		others = append(others, bills[i+1:]...)
		if err := b.ValidateAgainst(others); err != nil {
			return err
		}
	}
	return nil
}
