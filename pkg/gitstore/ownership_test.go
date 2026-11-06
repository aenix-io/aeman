package gitstore

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// docs/design/ownership.md, Storage: a planning card's file carries `team:`
// and `owner:` (a login); any card but a project card may carry
// `customers:` (customer ids). The roster is customers/<ulid>.yaml in the
// primary repository, `name:` and `rank:`. No schema bump.

func TestACardFileCarriesOwnerAndCustomers(t *testing.T) {
	in := CardFile{Card: board.Card{ItemID: "01JB4K2E7QZMX3R8V0N5T9WYC1", Title: "an epic", Type: board.TypeEpic,
		Team: "portal", Owner: "ann", Customers: []string{"01CUSTA", "01CUSTB"}, CreatedAt: "2026-11-06T09:00:00Z"}}
	data, err := EncodeCard(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"team: portal\n", "owner: ann\n", "customers: [01CUSTA, 01CUSTB]\n"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatalf("%q missing:\n%s", want, data)
		}
	}
	out, err := DecodeCard(in.Card.ItemID, data)
	if err != nil {
		t.Fatal(err)
	}
	if out.Card.Owner != "ann" || out.Card.Team != "portal" || !reflect.DeepEqual(out.Card.Customers, in.Card.Customers) {
		t.Fatalf("read back = %+v", out.Card)
	}
	again, _ := EncodeCard(out)
	if !bytes.Equal(data, again) {
		t.Fatalf("round trip is not the identity:\n%s\n---\n%s", data, again)
	}
}

func TestACustomerFileIsInTheLayout(t *testing.T) {
	if kind, ids := ParsePath(CustomerPath("01CUSTA")); kind != PathCustomer || ids[0] != "01CUSTA" {
		t.Fatalf("customers/01CUSTA.yaml = %v %v", kind, ids)
	}
	data, err := EncodeCustomer(CustomerFile{Name: "Hidora", Rank: "i", Created: "2026-11-06T09:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := DecodeCustomer(data)
	if err != nil || f.Name != "Hidora" || f.Rank != "i" {
		t.Fatalf("decoded %+v %v from:\n%s", f, err, data)
	}
}

// O5 — the roster: add, rename, reorder, delete. The board reads it in
// rank order; cards name a customer by id, so a rename reaches every card
// at once, and a delete takes the id off every card that names it, in ONE
// commit with the roster file.
func TestTheCustomerRoster(t *testing.T) {
	be, r := newBackend(t)
	ctx := ctxAs("kvaps")
	bd, _ := be.LoadBoard(ctx, "x")
	hidora, err := be.AddCustomer(ctx, bd, "Hidora")
	if err != nil {
		t.Fatal(err)
	}
	acme, err := be.AddCustomer(ctx, bd, "Acme")
	if err != nil {
		t.Fatal(err)
	}
	bd, _ = be.LoadBoard(ctx, "x")
	if got := names(bd); !reflect.DeepEqual(got, []string{"Hidora", "Acme"}) {
		t.Fatalf("the roster = %v, want Hidora then Acme", got)
	}
	if err := be.MoveCustomer(ctx, bd, acme.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := be.RenameCustomer(ctx, bd, hidora.ID, "Hidora SA"); err != nil {
		t.Fatal(err)
	}
	bd, _ = be.LoadBoard(ctx, "x")
	if got := names(bd); !reflect.DeepEqual(got, []string{"Acme", "Hidora SA"}) {
		t.Fatalf("after the move and the rename = %v", got)
	}

	card, _ := findByID(bd, "01JB4K2E7QZMX3R8V0N5T9WYC1")
	if err := be.SetCustomers(ctx, bd, card, []string{hidora.ID, acme.ID}); err != nil {
		t.Fatal(err)
	}
	bd, _ = be.LoadBoard(ctx, "x")
	card, _ = findByID(bd, card.ItemID)
	if !reflect.DeepEqual(card.Customers, []string{hidora.ID, acme.ID}) {
		t.Fatalf("the card's customers = %v", card.Customers)
	}
	before := r.Head()
	if err := be.DeleteCustomer(ctx, bd, hidora.ID); err != nil {
		t.Fatal(err)
	}
	bd, _ = be.LoadBoard(ctx, "x")
	card, _ = findByID(bd, card.ItemID)
	if !reflect.DeepEqual(card.Customers, []string{acme.ID}) || !reflect.DeepEqual(names(bd), []string{"Acme"}) {
		t.Fatalf("after the delete: card %v, roster %v", card.Customers, names(bd))
	}
	if c, _ := r.CommitObject(r.Head()); len(c.ParentHashes) != 1 || c.ParentHashes[0] != before {
		t.Fatal("the delete was not one commit")
	}
}

// And a planning card's owner is written with the card.
func TestAPlanningCardsOwnerIsWritten(t *testing.T) {
	be, _ := newBackend(t)
	ctx := context.Background()
	bd, _ := be.LoadBoard(ctx, "x")
	epic, err := be.CreateCard(ctx, bd, board.CreateInput{Title: "an epic", Type: board.TypeEpic})
	if err != nil {
		t.Fatal(err)
	}
	if err := be.SetOwner(ctx, bd, epic, "ann"); err != nil {
		t.Fatal(err)
	}
	if err := be.SetTeam(ctx, bd, epic, "portal"); err != nil {
		t.Fatal(err)
	}
	bd, _ = be.LoadBoard(ctx, "x")
	got, _ := findByID(bd, epic.ItemID)
	if got.Owner != "ann" || got.Team != "portal" {
		t.Fatalf("the epic = owner %q team %q", got.Owner, got.Team)
	}
}

func names(bd board.Board) []string {
	var out []string
	for _, c := range bd.Customers {
		out = append(out, c.Name)
	}
	return out
}
