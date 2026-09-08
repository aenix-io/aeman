import { describe, expect, it } from "vitest";
import { DEFAULT_SIZE, points, pointsOf, sizeFromWire } from "./size";
import type { Card } from "./providers/types";

// The scale doubles at each step so a week of S-work and a week of L-work can
// be compared at all. Mirrors board.Points and board.DefaultSize.
describe("points", () => {
  it("follows the scale", () => {
    expect(points("S")).toBe(1);
    expect(points("M")).toBe(2);
    expect(points("L")).toBe(4);
    expect(points("XL")).toBe(8);
  });

  it("is the scale, so the empty size is 0 on it", () => {
    expect(points(undefined)).toBe(0);
    expect(points("")).toBe(0);
  });

  // What a CARD weighs is another matter: unsized is M, the board's own
  // middle. Weighing it as nothing would say a full week is an empty one.
  it("weighs an unsized card as the default on a board", () => {
    expect(DEFAULT_SIZE).toBe("M");
    expect(pointsOf([], { itemId: "x" })).toBe(2);
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

  it("weighs a parent as its children, the unsized one at the default", () => {
    expect(pointsOf(cards, cards[0])).toBe(2 + 1 + 2);
  });

  it("weighs a child as its own size", () => {
    expect(pointsOf(cards, cards[1])).toBe(2);
  });

  it("stops weighing itself once it has subtasks, sized or not", () => {
    expect(pointsOf(cards, cards[4])).toBe(2);
  });

  it("weighs a card with no children as itself, unsized as the default", () => {
    expect(pointsOf(cards, { itemId: "lone", size: "M" })).toBe(2);
    expect(pointsOf(cards, { itemId: "lone" })).toBe(2);
  });
});

describe("what a review weighs", () => {
  // Mirrors board.TestAnUnsizedReviewCardWeighsS. A review is somebody
  // reading finished work and saying yes or no — S by the rubric — and the
  // board makes one for every card sent to review, so weighing them as M put
  // two points on a reviewer for each thing they were asked to look at.
  it("weighs an unsized review as S and an unsized card as M", () => {
    const review = { itemId: "r", reviewOf: "orig" } as Card;
    const plain = { itemId: "c" } as Card;
    expect(pointsOf([review, plain], review)).toBe(1);
    expect(pointsOf([review, plain], plain)).toBe(2);
  });

  it("keeps a size somebody gave a review", () => {
    const big = { itemId: "r", reviewOf: "orig", size: "L" } as Card;
    expect(pointsOf([big], big)).toBe(4);
  });

  it("weighs review CHILDREN as reviews under the umbrella rule", () => {
    const cards = [
      { itemId: "p" },
      { itemId: "k1", parent: "p", reviewOf: "x" },
      { itemId: "k2", parent: "p", reviewOf: "y" },
    ] as Card[];
    expect(pointsOf(cards, cards[0])).toBe(2);
  });
});
