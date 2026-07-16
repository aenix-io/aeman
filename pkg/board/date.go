package board

import "time"

// isoLayout is the yyyy-mm-dd date layout the boards orient by.
const isoLayout = "2006-01-02"

// TodayIso returns today's date as a local yyyy-mm-dd string. It mirrors todayIso
// in web/src/date.ts.
// boardLocation is the time zone the BOARD's days live in — one zone for
// every user and the server, or a Moscow teammate crossing their local
// midnight starts seeing "tomorrow's" cards while the lead is still on
// today's board. Defaults to the server's local zone; SetLocation installs
// the configured one (AEMAN_TZ).
var boardLocation = time.Local

// SetLocation installs the board time zone (name per IANA, e.g.
// "Europe/Berlin"). An unknown name is reported and leaves the zone as-is.
func SetLocation(name string) error {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return err
	}
	boardLocation = loc
	return nil
}

// LocationName reports the installed board time zone (IANA name or "Local").
func LocationName() string {
	return boardLocation.String()
}

func TodayIso() string {
	return time.Now().In(boardLocation).Format(isoLayout)
}

// LocalDateIso returns the local yyyy-mm-dd date for an ISO timestamp, or "" when
// the timestamp cannot be parsed. It mirrors localDateIso in web/src/date.ts.
func LocalDateIso(iso string) string {
	t, ok := parseTimestamp(iso)
	if !ok {
		return ""
	}
	return t.In(boardLocation).Format(isoLayout)
}

// ActiveOnDay reports whether `day` falls within a card's [start, finish] range.
// A missing bound collapses to the other date, so a card with a single date is
// active only on that day and a card with neither date is never on the day board.
// It mirrors activeOnDay in web/src/date.ts (from = start||finish,
// to = finish||start, from <= day <= to).
func ActiveOnDay(start, finish, day string) bool {
	from := start
	if from == "" {
		from = finish
	}
	if from == "" {
		return false
	}
	// The span runs from `start` to the later of start/finish, so a card whose
	// start day is past its sprint (added on a later day) still shows on its day.
	to := from
	if finish > from {
		to = finish
	}
	return from <= day && day <= to
}

// DaysSince returns the whole days between an ISO timestamp's local date and the
// `asOf` day (a yyyy-mm-dd string; "" means today). It never goes negative. It
// mirrors daysSince in web/src/date.ts.
func DaysSince(iso, asOf string) int {
	if iso == "" {
		return 0
	}
	then := LocalDateIso(iso)
	if then == "" {
		return 0
	}
	if asOf == "" {
		asOf = TodayIso()
	}
	t1, err1 := time.Parse(isoLayout, then)
	t2, err2 := time.Parse(isoLayout, asOf)
	if err1 != nil || err2 != nil {
		return 0
	}
	days := int(t2.Sub(t1) / (24 * time.Hour))
	if days < 0 {
		return 0
	}
	return days
}

// MondayOf returns the yyyy-mm-dd Monday of the week containing `iso`, or `iso`
// unchanged when it is not a parseable date. It mirrors mondayOf in
// web/src/date.ts.
func MondayOf(iso string) string {
	t, err := time.Parse(isoLayout, iso)
	if err != nil {
		return iso
	}
	// time.Weekday and JS getDay both run Sunday=0..Saturday=6, so (wd+6)%7 is the
	// number of days back to Monday.
	offset := (int(t.Weekday()) + 6) % 7
	return t.AddDate(0, 0, -offset).Format(isoLayout)
}

// AddDays shifts a yyyy-mm-dd date by delta days, returning yyyy-mm-dd, or `iso`
// unchanged when it is not a parseable date. It mirrors addDays in
// web/src/date.ts.
func AddDays(iso string, delta int) string {
	t, err := time.Parse(isoLayout, iso)
	if err != nil {
		return iso
	}
	return t.AddDate(0, 0, delta).Format(isoLayout)
}

// parseTimestamp parses an ISO timestamp the way the frontend's `new Date(iso)`
// does for the inputs aeman sees: an RFC 3339 timestamp or a bare yyyy-mm-dd.
func parseTimestamp(iso string) (time.Time, bool) {
	if iso == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", isoLayout} {
		if t, err := time.Parse(layout, iso); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
