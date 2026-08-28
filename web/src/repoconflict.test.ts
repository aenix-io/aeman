import { describe, expect, it } from "vitest";

import { offerableProjects, offerableTeams, rosterDomain } from "./domains";

// A card's project decides which repository it lives in, and its team does
// not get a say — so a team from another repository would sit on a card its
// people cannot read. The server refuses the pair; the pickers must not
// offer it in the first place.
describe("teams and projects of one repository", () => {
  const teams = ["", "backoffice", "founders", "cozystack"];
  const projects = ["backoffice", "strategy"];
  const teamDomains = { founders: "founders" };
  const projectDomains = { strategy: "founders" };

  it("names the repository an entry was declared in, the primary being unnamed", () => {
    expect(rosterDomain(teamDomains, "founders")).toBe("founders");
    expect(rosterDomain(teamDomains, "backoffice")).toBe("");
    expect(rosterDomain(undefined, "founders")).toBe("");
    expect(rosterDomain(teamDomains, "")).toBe("");
  });

  it("offers only the teams of the project's repository", () => {
    expect(offerableTeams(teams, teamDomains, projectDomains, "backoffice")).toEqual([
      "",
      "backoffice",
      "cozystack",
    ]);
    expect(offerableTeams(teams, teamDomains, projectDomains, "strategy")).toEqual([
      "",
      "founders",
    ]);
  });

  it("constrains nothing when the card is under no project", () => {
    expect(offerableTeams(teams, teamDomains, projectDomains, "")).toEqual(teams);
  });

  it("offers only the projects of the team's repository", () => {
    expect(offerableProjects(projects, projectDomains, teamDomains, "founders")).toEqual([
      "strategy",
    ]);
    expect(offerableProjects(projects, projectDomains, teamDomains, "backoffice")).toEqual([
      "backoffice",
    ]);
    expect(offerableProjects(projects, projectDomains, teamDomains, "")).toEqual(projects);
  });

  it("offers everything on a board of one repository (and to an older server)", () => {
    expect(offerableTeams(teams, undefined, undefined, "backoffice")).toEqual(teams);
    expect(offerableProjects(projects, undefined, undefined, "founders")).toEqual(projects);
  });

  it("keeps the option a card already carries, so a legacy pair can be fixed", () => {
    // A card written before the guard: team founders under project
    // backoffice. The menu still shows what it has — otherwise the chip
    // reads as "no team" and the only way out is invisible.
    expect(
      offerableTeams(teams, teamDomains, projectDomains, "backoffice", "founders"),
    ).toEqual(["", "backoffice", "founders", "cozystack"]);
  });
});
