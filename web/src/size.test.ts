import { describe, expect, it } from "vitest";
import { points, pointsOf, sizeFromWire } from "./size";

// The scale doubles at each step so a week of S-work and a week of L-work
// can be compared at all; a card nobody sized weighs nothing. Mirrors
// board.Points.
describe("points", () => {
  it("follows the scale", () => {
    expect(points("S")).toBe(1);
    expect(points("M")).toBe(2);
    expect(points("L")).toBe(4);
    expect(points("XL")).toBe(8);
  });

  it("weighs an unsized card as nothing", () => {
    expect(points(undefined)).toBe(0);
    expect(points("")).toBe(0);
  });
});

// The letter is what travels; a server that sends anything else — or nothing
// — leaves the card unsized rather than sized as something unknown.
describe("sizeFromWire", () => {
  it("reads the four letters", () => {
    expect(sizeFromWire("XL")).toBe("XL");
    expect(sizeFromWire("S")).toBe("S");
  });

  it("reads anything else as unsized", () => {
    expect(sizeFromWire("")).toBeUndefined();
    expect(sizeFromWire(undefined)).toBeUndefined();
    expect(sizeFromWire("large")).toBeUndefined();
  });
});

// The umbrella rule, mirrored from board.PointsOf: a parent with sized
// subtasks weighs what its children weigh, so the total never counts twice —
// and children nobody sized yet leave the parent's own estimate standing.
describe("pointsOf", () => {
  const cards = [
    { itemId: "p", size: "L" as const },
    { itemId: "k1", parent: "p", size: "M" as const },
    { itemId: "k2", parent: "p", size: "S" as const },
    { itemId: "k3", parent: "p" },
    { itemId: "q", size: "XL" as const },
    { itemId: "q1", parent: "q" },
  ];

  it("weighs a parent with sized children as their sum", () => {
    expect(pointsOf(cards, cards[0])).toBe(3);
  });

  it("weighs a child as its own size", () => {
    expect(pointsOf(cards, cards[1])).toBe(2);
  });

  it("keeps the parent's own size until a child is sized", () => {
    expect(pointsOf(cards, cards[4])).toBe(8);
  });

  it("weighs a card with no children as itself", () => {
    expect(pointsOf(cards, { itemId: "lone", size: "M" })).toBe(2);
    expect(pointsOf(cards, { itemId: "lone" })).toBe(0);
  });
});
