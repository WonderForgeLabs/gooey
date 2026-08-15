package main

// The shelf. Forty records is enough that most jumps leave the visible
// window — which is the interesting case for type-ahead over image rows,
// since a cover is four cells tall and a normal terminal shows six of
// them.
//
// The titles are chosen so the alphabet is uneven: eight begin with A,
// six with B, one with X, none with Q. Type-ahead's cycling behaviour is
// invisible on a list where every initial is unique, and its "no match"
// state is invisible on a list where every letter matches something.
var shelf = []struct {
	title  string
	artist string
	year   int
}{
	{"Aftertone", "Vela Mercer", 1998},
	{"Alabaster", "The Quiet Hours", 2004},
	{"Almanac", "Ridgeway", 2011},
	{"Amber Room", "Sable & Co", 1987},
	{"Anemone", "Halva", 2019},
	{"Antechamber", "Vela Mercer", 2001},
	{"Aphelion", "North Circular", 2016},
	{"Argonaut", "Fen Lantern", 1993},
	{"Basalt", "Ridgeway", 2007},
	{"Beacon Hill", "The Quiet Hours", 2012},
	{"Bellwether", "Marisol Vance", 1995},
	{"Blackthorn", "Fen Lantern", 2018},
	{"Bramblewick", "Halva", 2002},
	{"Brimstone", "North Circular", 2021},
	{"Cinder Lane", "Sable & Co", 1999},
	{"Coastal Road", "Marisol Vance", 2013},
	{"Cormorant", "Vela Mercer", 2009},
	{"Driftwood", "Ridgeway", 1991},
	{"Dulcimer", "Halva", 2015},
	{"Everlight", "The Quiet Hours", 2020},
	{"Fathom", "Fen Lantern", 1989},
	{"Foxglove", "Marisol Vance", 2006},
	{"Gantry", "North Circular", 2003},
	{"Halcyon", "Sable & Co", 2017},
	{"Harrowgate", "Ridgeway", 1996},
	{"Ironwood", "Vela Mercer", 2010},
	{"Jetsam", "Halva", 1994},
	{"Kestrel", "Fen Lantern", 2022},
	{"Lantern Hill", "Marisol Vance", 2000},
	{"Meridian", "The Quiet Hours", 1992},
	{"Nightjar", "North Circular", 2014},
	{"Oriel", "Sable & Co", 2008},
	{"Pennant", "Ridgeway", 1985},
	{"Rookery", "Vela Mercer", 2023},
	{"Saltmarsh", "Halva", 1997},
	{"Sextant", "Fen Lantern", 2005},
	{"Thistledown", "Marisol Vance", 2024},
	{"Undertow", "The Quiet Hours", 1990},
	{"Wayfarer", "North Circular", 2012},
	{"Xebec", "Sable & Co", 1986},
}

// catalogue builds the records, drawing each cover exactly once.
// Building the picture once matters: the covers are re-projected on
// every layout pass, and generating one per projection would hand the
// row a different image.Image each frame, which the placement diff would
// read as "the picture changed" and re-transmit forever.
func catalogue() []record {
	out := make([]record, 0, len(shelf))
	for _, s := range shelf {
		out = append(out, record{
			Title:  s.title,
			Artist: s.artist,
			Year:   s.year,
			img:    coverOf(s.title, s.year),
		})
	}
	return out
}
