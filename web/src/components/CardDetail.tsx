import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type {
  Board,
  Card as CardModel,
  CardEvent,
  CardLog,
  CardType,
  CustomerRef,
  LineageRef,
  Note,
  Provider,
  SizeKey,
  ZoneKey,
} from "../providers/types";
import { keepPlaced } from "../backlog";
import { ZONES, ZONE_ORDER } from "../zones";
import { SIZES, SIZE_ORDER } from "../size";
import { STAGES, barColor, clampProgress, clampsProgress } from "../stages";
import { ProgressSlider } from "./ProgressSlider";
import { Dropdown } from "./Dropdown";
import { eventLabel } from "../eventlog";
import { localDateIso, todayIso } from "../date";
import { reviewerCandidates } from "../domains";
import { ownerLabel, ownerOf, planOwnerOf, planOwnPatch, type Owner } from "../owner";
import { customerChips, inheritedTeamOf } from "../ownership";
import { CustomerField } from "./CustomerField";
import { teamColor } from "../avatar";
import { avatarsFrom, displayName, namesFrom, type Avatars, type Names } from "../users";
import type { RemoveChoice } from "../removal";
import {
  KIND_LABEL,
  STATUS_LABEL,
  allowedParentKinds,
  childrenNote,
  hierarchyChoices,
  kindOf,
  planningRemoval,
  cardsAfterDelete,
  statusOf,
  type RoadmapKind,
} from "../roadmap";
import { isPlanning, isSubtask, subtaskParent } from "../subtasks";
import { Avatar } from "./Avatar";
import { DescriptionEditor } from "./DescriptionEditor";
import { KindIcon } from "./KindIcon";
import { PlanTree, type PendingAdd } from "./PlanTree";
import { CardIdentity } from "./CardIdentity";
import { placementAnswer, drawerEscapes, joinsTree } from "../detail";
import { OwnerPicker } from "./OwnerPicker";
import { ParentPicker } from "./ParentPicker";
import { DependsOn } from "./DependsOn";
import { dependenciesToFetch, withDependency, withFetched, withoutDependency } from "../dependencies";
import { RemoveChoiceDialog } from "./RemoveChoiceDialog";
import { notTombstoned, type Tombstones } from "../tombstones";

interface CardDetailProps {
  card: CardModel;
  board: Board;
  provider: Provider;
  onClose: () => void;
  reload: () => void;
  patchCard: (
    itemId: string,
    patch: Partial<CardModel> | ((c: CardModel) => Partial<CardModel>),
  ) => void;
  /** The board's own card list, for a child made or removed here. Absent,
   *  the pane keeps its own list until the watch catches up. */
  addCard?: (card: CardModel) => void;
  removeCard?: (itemId: string) => void;
  /** A breadcrumb was followed: show that card on the Roadmap. */
  onReveal?: (uid: string) => void;
  /** A crumb, a child row, the task a subtask rides: open that card here,
   *  in this drawer, with a way back. */
  onOpenCard?: (card: CardModel) => void;
  /** There is a card to go back to (the pane was opened from another). */
  onBack?: () => void;
  /** The board's Card watch frames, relayed: the pane keeps its copy of the
   *  hierarchy in step with them. */
  subscribeCardFrames?: (fn: (uid: string, deleted: boolean, card?: CardModel) => void) => () => void;
  /** The cards this tab deleted (tombstones.ts): a listing answered after the
   *  DELETED still carries one, and the tree must not draw it again. */
  tombstones?: Tombstones;
  /** The card's address on this board (deeplink.cardLink), for its
   *  identity line: the id and the link to copy. */
  cardLinkFor?: (uid: string) => string;
  /** A write the server refused, in its own words (a parent of the wrong
   *  level, another repository). */
  onError?: (message: string) => void;
  /** A customer the drawer added to the roster (ownership.md): the board's
   *  roster takes it at once. */
  onCustomerAdded?: (customer: CustomerRef) => void;
}

const errMessage = (err: unknown) => (err instanceof Error ? err.message : String(err));

// The roadmap listing the panes read, shared by every pane opened on the
// board for a minute, so going from card to card does not fetch it again.
// Any watch frame and any write made from a pane makes it stale.
const LISTING_TTL_MS = 60_000;
const listings = new Map<string, { cards: CardModel[]; at: number }>();
function cachedListing(key: string, now: number): CardModel[] | null {
  const l = listings.get(key);
  return l && now - l.at <= LISTING_TTL_MS ? l.cards : null;
}
function storeListing(key: string, cards: CardModel[], now: number): void {
  listings.set(key, { cards, at: now });
}
function dropListings(): void {
  listings.clear();
}


/** CardDetail is the card's pane: a drawer that slides in from the right and
 *  stands over whichever board the card was met on, laid out like an issue
 *  page — the title and its breadcrumb up top, the description and the
 *  activity down the main column, every field the card has in a sidebar of
 *  labelled rows, and the actions that act on the whole card at the bottom
 *  of it. Which rows there are is the card's KIND's to say: a task has a
 *  band and a size, a planning card a level and a standing — the server
 *  refuses the rest, so the pane does not offer it.
 *
 *  On a RECORD — a card as it stood on a day its team has moved past — it
 *  reads instead: the boxes are disabled and there is nothing to save, since
 *  a change made from there would land on today's card while the reader is
 *  looking at a picture of a day that ended (G60). */
export function CardDetail({
  card,
  board,
  provider,
  onClose,
  reload,
  patchCard,
  addCard,
  removeCard,
  onReveal,
  onOpenCard,
  onBack,
  cardLinkFor,
  subscribeCardFrames,
  tombstones,
  onError,
  onCustomerAdded,
}: CardDetailProps) {
  // A record: what the card was on a day its team has moved past. Nothing
  // here is editable, and the pane says so rather than pretending.
  const isRecord = !!card.asOf;
  const temp = card.itemId.startsWith("tmp-");
  const [title, setTitle] = useState(card.title);
  const planning = isPlanning(card);
  const kind = kindOf(card);
  const task = !planning;
  const subtask = isSubtask(card);

  // The hierarchy around this card: what it may sit under, and what sits
  // under it. Both come from the roadmap listing, which holds every planning
  // card and every card under one — the day boards never deliver a planning
  // card, so a pane opened from one has only the lineage until this
  // arrives. Fetched once per card, when the pane needs it: a planning
  // card's children at once, a task's parent options when the picker opens.
  const [hierarchy, setHierarchy] = useState<{ forCard: string; cards: CardModel[] } | null>(null);
  const [wantsOptions, setWantsOptions] = useState(false);
  // A card that waits for others names them from the same listing.
  const wantsHierarchy = planning || wantsOptions || (card.dependsOn?.length ?? 0) > 0;
  useEffect(() => {
    if (!wantsHierarchy || temp || hierarchy?.forCard === card.itemId) {
      return;
    }
    // A plan item's "Under it" and a task's parent picker read the same
    // listing: the Roadmap's.
    const key = "plan";
    const cached = cachedListing(key, Date.now());
    if (cached) {
      setHierarchy({ forCard: card.itemId, cards: cached });
      return;
    }
    let dropped = false;
    void provider
      .listCards({ view: "roadmap" })
      .then((l) => {
        const listed = tombstones ? notTombstoned(l.cards, tombstones) : l.cards;
        storeListing(key, listed, Date.now());
        if (!dropped) {
          setHierarchy({ forCard: card.itemId, cards: listed });
        }
      })
      .catch(() => {
        if (!dropped) {
          setHierarchy({ forCard: card.itemId, cards: [] });
        }
      });
    return () => {
      dropped = true;
    };
  }, [wantsHierarchy, card.itemId, provider, hierarchy?.forCard, temp, planning, tombstones]);
  // The board's watch keeps the pane's copy of the hierarchy in step: a card
  // deleted outright leaves it, a card changed is taken as it now is, and
  // any change makes the shared listing stale for the next pane.
  useEffect(() => {
    if (!subscribeCardFrames) {
      return;
    }
    return subscribeCardFrames((uid, deleted, framed) => {
      dropListings();
      setHierarchy((h) => {
        if (!h) {
          return h;
        }
        if (deleted) {
          return framed && !framed.title ? { ...h, cards: h.cards.filter((c) => c.itemId !== uid) } : h;
        }
        if (framed && h.cards.some((c) => c.itemId === uid)) {
          return { ...h, cards: h.cards.map((c) => (c.itemId === uid ? { ...c, ...framed, description: c.description, notes: c.notes } : c)) };
        }
        // A card made elsewhere — another tab, an agent — under the card
        // the drawer shows joins its tree (detail.joinsTree).
        return framed && joinsTree(h.cards, framed, h.forCard) ? { ...h, cards: [...h.cards, framed] } : h;
      });
    });
  }, [subscribeCardFrames]);
  const around = hierarchy?.forCard === card.itemId ? hierarchy.cards : null;
  const [fetchedDeps, setFetchedDeps] = useState<readonly CardModel[]>([]);
  // The ids this drawer card has asked for already, answered or not
  // (dependenciesToFetch): asked once, never again for the same card.
  const triedDeps = useRef<{ forCard: string; ids: Set<string> }>({ forCard: card.itemId, ids: new Set() });
  if (triedDeps.current.forCard !== card.itemId) {
    triedDeps.current = { forCard: card.itemId, ids: new Set() };
  }
  // What the board itself holds is fresher than the fetched listing for the
  // cards it has (the watch keeps it so); the listing fills in the rest.
  const known = useMemo(() => {
    const byId = new Map<string, CardModel>();
    for (const c of around ?? []) {
      byId.set(c.itemId, c);
    }
    for (const c of fetchedDeps) {
      if (!byId.has(c.itemId)) {
        byId.set(c.itemId, c);
      }
    }
    for (const c of board.cards) {
      byId.set(c.itemId, c);
    }
    return [...byId.values()];
  }, [around, board.cards, fetchedDeps]);
  // A dependency the listing does not carry — finished work outside the
  // tree, a card of another view — is fetched by id once, so its row has a
  // title and a status rather than a bare uid.
  useEffect(() => {
    if (around === null || temp) {
      return;
    }
    const have = new Set([...around, ...board.cards, ...fetchedDeps].map((c) => c.itemId));
    const tried = triedDeps.current;
    const missing = dependenciesToFetch(card.dependsOn, have, tried.ids);
    if (missing.length === 0) {
      return;
    }
    for (const id of missing) {
      tried.ids.add(id);
    }
    let dropped = false;
    void Promise.all(missing.map((id) => provider.getCard(id).catch(() => null))).then((got) => {
      if (!dropped) {
        setFetchedDeps((prev) => withFetched(prev, got));
      } else {
        // The effect was superseded before the answer came: let the next
        // run ask again rather than lose the id for this card.
        for (const id of missing) {
          tried.ids.delete(id);
        }
      }
    });
    return () => {
      dropped = true;
    };
  }, [around, board.cards, card.dependsOn, fetchedDeps, provider, temp]);

  // The parent as the pane shows it: the last crumb of the lineage.
  const parentCrumb = card.lineage?.[card.lineage.length - 1];
  const offersParent = !isRecord && allowedParentKinds(kind).length > 0 && !subtask;

  // Going to another card from here — a crumb, the task a subtask rides, a
  // row under a planning card — opens it in this drawer; the caller keeps
  // the way back. A card the board does not hold (an ancestor met from a
  // day board) is fetched by id.
  const openById = (uid: string) => {
    if (!onOpenCard) {
      return;
    }
    const here = known.find((c) => c.itemId === uid);
    if (here) {
      onOpenCard(here);
      return;
    }
    void provider
      .getCard(uid)
      .then(onOpenCard)
      .catch((err: unknown) => onError?.(errMessage(err)));
  };

  // The breadcrumb: the card's ancestors, root first — and on a REVIEW card
  // the task it reviews as the last crumb (status.reviewed), opened like the
  // others. A review hangs under nothing of its own; its chain is its task's.
  const crumbs: LineageRef[] = [
    ...(card.lineage ?? []),
    ...(card.reviewed ? [{ uid: card.reviewed.uid, title: card.reviewed.title }] : []),
  ];

  // ---- whose this is: the owner, and the reviewer when it is on review ----
  const members = useMemo(() => board.members.map((m) => m.login), [board.members]);
  const avatars = useMemo(() => avatarsFrom(board.members), [board.members]);
  const names = useMemo(() => namesFrom(board.members), [board.members]);
  const owner = ownerOf(card);
  const setOwner = (next: Owner) => {
    if (next.team === owner.team && next.person === owner.person) {
      return;
    }
    const before: Partial<CardModel> = {
      team: card.team,
      assignees: card.assignees,
      startDate: card.startDate,
      day: card.day,
      zone: card.zone,
      sprintStart: card.sprintStart,
      parked: card.parked,
    };
    // The team and the person at once; the rest — the day, the band, the
    // sprint, the shelf — is the gesture's answer (hierarchy.md, "Owner").
    patchCard(card.itemId, { team: next.team || undefined, assignees: next.person ? [next.person] : [] });
    void provider
      .ownCard(card.itemId, next.team, next.person)
      .then((updated) =>
        patchCard(card.itemId, {
          ...placementAnswer(updated),
          assignees: updated.assignees,
          startDate: updated.startDate,
          day: updated.day,
          zone: updated.zone,
          parked: updated.parked,
        }),
      )
      .catch((err: unknown) => {
        patchCard(card.itemId, before);
        onError?.(errMessage(err));
      });
  };
  // A card on review has a REVIEW card linked to it, and that card's person
  // is the reviewer; the pane is opened on either side of the link. The
  // original is the one the review doors are knocked on.
  const originalId = card.reviewOf ?? card.itemId;
  const reviewCard = card.reviewOf ? card : board.cards.find((c) => c.reviewOf === card.itemId);
  const onReview = !!card.reviewOf || card.stage === "review" || !!reviewCard;
  const reviewer = reviewCard?.assignees[0] ?? "";
  const reviewers = useMemo(() => reviewerCandidates(members, board.domains ?? [], card.domain), [members, board.domains, card.domain]);
  const [reviewerOpen, setReviewerOpen] = useState(false);
  const reviewerRef = useRef<HTMLSpanElement | null>(null);
  const setReviewer = (login: string | null) => {
    setReviewerOpen(false);
    if ((login ?? "") === reviewer) {
      return;
    }
    if (login === null) {
      if (!reviewCard) {
        return;
      }
      removeCard?.(reviewCard.itemId);
      void provider
        .removeReviewer(originalId)
        .then((original) => {
          addCard?.(original);
          if (card.reviewOf) {
            onClose();
          }
        })
        .catch((err: unknown) => {
          addCard?.(reviewCard);
          onError?.(errMessage(err));
        });
      return;
    }
    const before = reviewCard?.assignees;
    if (reviewCard) {
      patchCard(reviewCard.itemId, { assignees: [login] });
    }
    void provider
      .sendToReview(originalId, login)
      .then((made) => {
        if (reviewCard) {
          patchCard(reviewCard.itemId, { assignees: made.assignees });
        } else {
          addCard?.(made);
        }
      })
      .catch((err: unknown) => {
        if (reviewCard && before) {
          patchCard(reviewCard.itemId, { assignees: before });
        }
        onError?.(errMessage(err));
      });
  };

  // ---- a task's subtasks: what rides it, and one more ----
  const subtasks = useMemo(
    () => (task && !subtask ? known.filter((c) => subtaskParent(c) === card.itemId || (c.parent === card.itemId && !isPlanning(c))) : []),
    [known, task, subtask, card.itemId],
  );
  const [addingSub, setAddingSub] = useState(false);
  const [subTitle, setSubTitle] = useState("");
  const [pendingSubs, setPendingSubs] = useState<string[]>([]);
  const addSubtask = () => {
    const t = subTitle.trim();
    if (!t) {
      setAddingSub(false);
      return;
    }
    setSubTitle("");
    setPendingSubs((p) => [...p, t]);
    const today = todayIso();
    // The way the Team and Me boards group a card under another: the
    // parent's team, band and person come along (the server cascades them,
    // subtask parity), and the day is the parent's own or today.
    void provider
      .createCard({
        title: t,
        team: card.team ?? null,
        zone: card.zone ?? "gray",
        start: card.startDate ?? today,
        day: card.day ?? card.startDate ?? today,
        assigneeLogin: card.assignees[0] ?? null,
        parent: card.itemId,
      })
      .then((made) => {
        setPendingSubs((p) => p.filter((x) => x !== t));
        const withLineage: CardModel = made.lineage
          ? made
          : { ...made, lineage: [...(card.lineage ?? []), { uid: card.itemId, type: card.type, title: card.title }] };
        dropListings();
        setHierarchy((h) => (h && h.forCard === card.itemId ? { ...h, cards: [...h.cards, withLineage] } : h));
        addCard?.(withLineage);
      })
      .catch((err: unknown) => {
        setPendingSubs((p) => p.filter((x) => x !== t));
        onError?.(errMessage(err));
      });
  };

  const setParent = (next: CardModel | null) => {
    const nextId = next?.itemId ?? "";
    if ((card.parent ?? "") === nextId) {
      return;
    }
    const before = { parent: card.parent, lineage: card.lineage };
    // The optimistic lineage: the parent's own plus the parent, so the
    // breadcrumb and the Roadmap's tree read right before the server answers.
    patchCard(card.itemId, {
      parent: nextId || undefined,
      lineage: next ? [...(next.lineage ?? []), { uid: next.itemId, type: next.type, title: next.title }] : undefined,
    });
    void provider
      .patchCard(card.itemId, { parent: nextId })
      // Filed under another plan item it may stand under another team, in
      // that team's sprint (O13): take where the server put it.
      .then((updated) => patchCard(card.itemId, placementAnswer(updated)))
      .catch((err: unknown) => {
        patchCard(card.itemId, before);
        onError?.(errMessage(err));
      });
  };

  // What this card waits for: the whole list is written, the drawer shows
  // it at once and takes it back when the server refuses (a cycle, a target
  // it may not wait for), in the server's words.
  const wantDependencyOptions = useCallback(() => setWantsOptions(true), []);
  const setDependsOn = (next: string[]) => {
    const before: Partial<CardModel> = { dependsOn: card.dependsOn };
    patchCard(card.itemId, { dependsOn: next.length ? next : undefined });
    void provider
      .patchCard(card.itemId, { dependsOn: next })
      .then((updated) =>
        patchCard(card.itemId, {
          dependsOn: updated.dependsOn,
          locked: updated.locked,
          blockedBy: updated.blockedBy,
          hiddenBlockers: updated.hiddenBlockers,
        }),
      )
      .catch((err: unknown) => {
        patchCard(card.itemId, before);
        onError?.(errMessage(err));
      });
  };

  // A planning card's dates are its bar on the Roadmap; a task's are its
  // placement on the day boards
  // and are moved there, so here they only read.
  const editsDates = !isRecord && !task;
  const setDates = (start: string, end: string) => {
    if (start === (card.startDate ?? "") && end === (card.day ?? "")) {
      return;
    }
    const before = { startDate: card.startDate, day: card.day };
    patchCard(card.itemId, { startDate: start || undefined, day: end || undefined });
    void provider
      .patchCard(card.itemId, { dates: { start, end } })
      .then((updated) => patchCard(card.itemId, { startDate: updated.startDate, day: updated.day }))
      .catch((err: unknown) => {
        patchCard(card.itemId, before);
        onError?.(errMessage(err));
      });
  };

  // What sits under a planning card: the Roadmap's tree, rooted one level
  // below this card (PlanTree), over the hierarchy the pane holds.
  const hasChildren = useMemo(() => known.some((c) => c.parent === card.itemId), [known, card.itemId]);

  // ---- making something under a planning card, at any level of its tree ----
  const [pendingAdds, setPendingAdds] = useState<PendingAdd[]>([]);
  const addChild = (t: string, level: CardType | "task", parent: string) => {
    const title = t.trim();
    if (!title) {
      return;
    }
    const pendingId = `pending-${Date.now()}-${Math.random().toString(36).slice(2)}`;
    setPendingAdds((p) => [...p, { id: pendingId, title, parent }]);
    const type = level === "task" ? undefined : level;
    void provider
      .createCard({ title, type, parent, underPlanning: level === "task" })
      .then((made) => {
        setPendingAdds((p) => p.filter((x) => x.id !== pendingId));
        dropListings();
        setHierarchy((h) => (h && h.forCard === card.itemId ? { ...h, cards: [...h.cards, made] } : h));
        addCard?.(made);
      })
      .catch((err: unknown) => {
        setPendingAdds((p) => p.filter((x) => x.id !== pendingId));
        onError?.(errMessage(err));
      });
  };

  // ---- the × on a child, and the actions on the card itself ----
  // One dialog for all of it: the Team board's own, with the hierarchy's two
  // answers — delete, or only unlink (roadmap.hierarchyChoices).
  const [choosing, setChoosing] = useState<{ card: CardModel; choices: RemoveChoice[] } | null>(null);
  const answer = (target: CardModel, choice: RemoveChoice) => {
    // Detach a child of a planning card, or ungroup a subtask: the same
    // write — the link goes, the card stays where it is.
    if (choice === "detach" || choice === "ungroup") {
      const before = { parent: target.parent, lineage: target.lineage, parked: target.parked };
      // Out of its group or its plan with no place of its own, the server
      // parks it (K1, keepPlaced); the optimistic card says the same.
      const freed = { parent: undefined, lineage: undefined, ...keepPlaced(target, false) };
      patchCard(target.itemId, freed);
      dropListings();
      setHierarchy((h) =>
        h ? { ...h, cards: h.cards.map((c) => (c.itemId === target.itemId ? { ...c, ...freed } : c)) } : h,
      );
      void provider.patchCard(target.itemId, { parent: "" }).catch((err: unknown) => {
        patchCard(target.itemId, before);
        onError?.(errMessage(err));
      });
      return;
    }
    // Delete: a planning card's children move up to its own parent, as the
    // server does (reparentChildren); a task's subtasks go with it.
    for (const m of planningRemoval(known, target.itemId)) {
      patchCard(m.itemId, { parent: m.parent, lineage: m.parent ? target.lineage : undefined });
    }
    removeCard?.(target.itemId);
    // The same moves in the pane's own copy of the hierarchy, or a child
    // the board does not hold keeps a dead parent and leaves the tree.
    dropListings();
    setHierarchy((h) => (h ? { ...h, cards: cardsAfterDelete(h.cards, target.itemId) } : h));
    void provider.deleteCard(target.itemId).catch((err: unknown) => {
      onError?.(errMessage(err));
      reload();
    });
    if (target.itemId === card.itemId) {
      onClose();
    }
  };

  // Done and back: done clears the stage
  // and fills 100; the way back restores what done overwrote — the server
  // reads the pre-done progress from the card's own log.
  const complete = statusOf(card) === "done";
  const setDone = (done: boolean) => {
    const prev = { stage: card.stage, progress: card.progress };
    if (done) {
      patchCard(card.itemId, { stage: card.stage === "recurrent" ? "recurrent" : undefined, progress: 100 });
      void provider
        .patchCard(card.itemId, { stage: "done" })
        .then((updated) => patchCard(card.itemId, { stage: updated.stage, progress: updated.progress }))
        .catch((err: unknown) => {
          patchCard(card.itemId, prev);
          onError?.(errMessage(err));
        });
      return;
    }
    patchCard(card.itemId, { stage: undefined });
    void provider
      .reopen(card.itemId)
      .then((updated) => patchCard(card.itemId, { stage: updated.stage, progress: updated.progress }))
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        onError?.(errMessage(err));
      });
  };

  // Progress moves by dragging the bar's handle, the way it does on a card of
  // the day boards: the same slider, the same 10% grid, the same clamp for a
  // review/locked card. The server answers with the stage it settled on
  // (100 reads as done; a clamped stage holds the band), and that is what
  // stays on the card.
  const doneish = card.stage === "done" || (!card.stage && (card.progress ?? 0) >= 100);
  const progressValue = clampProgress(card.stage, doneish ? 100 : (card.progress ?? 0));
  const setProgress = (next: number) => {
    const prev = { stage: card.stage, progress: card.progress };
    patchCard(card.itemId, { progress: next });
    void provider
      .patchCard(card.itemId, { progress: next })
      .then((updated) => patchCard(card.itemId, { stage: updated.stage, progress: updated.progress }))
      .catch((err: unknown) => {
        patchCard(card.itemId, prev);
        onError?.(errMessage(err));
      });
  };

  // ---- ownership (ownership.md): a plan item's team and owner, any card's
  // customers, and the roster they come from ----
  const planOwner = planOwnerOf(card);
  const inheritedTeam = inheritedTeamOf(card, known);
  const setPlanOwner = (next: Owner) => {
    const change = planOwnPatch(planOwner, next);
    if (!change) {
      return;
    }
    const before: Partial<CardModel> = { team: card.team, owner: card.owner };
    patchCard(card.itemId, { team: next.team || undefined, owner: next.person || undefined });
    void provider
      .patchCard(card.itemId, change)
      .then((updated) => patchCard(card.itemId, { ...placementAnswer(updated), owner: updated.owner }))
      .catch((err: unknown) => {
        patchCard(card.itemId, before);
        onError?.(errMessage(err));
      });
  };
  const setCustomers = (ids: string[]) => {
    const before: Partial<CardModel> = { customers: card.customers };
    patchCard(card.itemId, { customers: ids.length ? ids : undefined });
    void provider
      .patchCard(card.itemId, { customers: ids })
      .then((updated) =>
        patchCard(card.itemId, { customers: updated.customers, effectiveCustomers: updated.effectiveCustomers }),
      )
      .catch((err: unknown) => {
        patchCard(card.itemId, before);
        onError?.(errMessage(err));
      });
  };
  // A customer the roster lacks is added to it first, then named on the card.
  const createCustomer = (name: string) => {
    void provider
      .addCustomer(name)
      .then((cu) => {
        onCustomerAdded?.(cu);
        setCustomers([...(card.customers ?? []), cu.id]);
      })
      .catch((err: unknown) => onError?.(errMessage(err)));
  };

  const [editingTitle, setEditingTitle] = useState(false);
  // Esc closes this card — the pane is a reader as often as an editor, and
  // reaching for the mouse to leave a card you only opened to look at is a
  // small tax paid over and over. The key is taken on the BACKDROP rather
  // than on the window: a dialog opened over the card answers its own Esc,
  // and the card underneath must not close at the same time. The title
  // editor answers first (it cancels the rename), so a card being renamed is
  // not closed out from under the typing.
  const backdropRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const node = backdropRef.current;
    if (node && !node.contains(document.activeElement)) {
      node.focus();
    }
  }, []);
  // What kind of work this is. It is set by which area of the Team board a
  // card stands in, which is no help when the card is met anywhere else —
  // and on the Backlog board a column is a team, so there is no area to
  // drop it into at all.
  const zoneRef = useRef<HTMLSpanElement | null>(null);
  const [zoneOpen, setZoneOpen] = useState(false);
  const setZone = (zone: ZoneKey) => {
    setZoneOpen(false);
    if (zone === card.zone) {
      return;
    }
    const before = card.zone;
    patchCard(card.itemId, { zone });
    void provider.patchCard(card.itemId, { zone }).catch((err: unknown) => {
      patchCard(card.itemId, { zone: before });
      onError?.(errMessage(err));
    });
  };

  // What the card WEIGHS — the decision a daily sync makes so the person
  // leaves with a plan that fits. The same letter again takes it back.
  const setSize = (size: SizeKey) => {
    const next: SizeKey | "" = size === card.size ? "" : size;
    const before = card.size;
    patchCard(card.itemId, { size: next || undefined });
    void provider.patchCard(card.itemId, { size: next }).catch((err: unknown) => {
      patchCard(card.itemId, { size: before });
      onError?.(errMessage(err));
    });
  };

  const [description, setDescription] = useState(card.description ?? "");
  // dirty marks the draft as the user's: once they typed, nothing arriving
  // from the board (a background re-list, the lazy body fetch, a watch frame)
  // may overwrite it. bodyFailed gates Save when the body never arrived.
  const [dirty, setDirty] = useState(false);
  const [bodyFailed, setBodyFailed] = useState(false);
  const [log, setLog] = useState<CardLog | null>(null);
  const [tab, setTab] = useState<"details" | "activity">("details");
  const bodyLoaded = card.description !== undefined;

  // Reset local edit state ONLY when a different card is opened — the card
  // object itself churns underneath the pane (watch frames, re-lists, the
  // body fetch below), and none of that may wipe what the user is typing.
  useEffect(() => {
    setTitle(card.title);
    setDescription(card.description ?? "");
    setDirty(false);
    setBodyFailed(false);
    setEditingTitle(false);
    setTab("details");
    setLog(null);
    setWantsOptions(false);
    setAddingSub(false);
    setSubTitle("");
    setReviewerOpen(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [card.itemId]);

  // Listings are board rows without the body: fetch it when the pane opens
  // on one. The result lands in the board (so every pane agrees) and into the
  // draft — unless the user already typed. A failure surfaces and keeps Save
  // locked instead of silently offering to save "" over the real text.
  useEffect(() => {
    if (bodyLoaded || temp) {
      return;
    }
    let dropped = false;
    void provider
      .getCard(card.itemId)
      .then((full) => {
        if (dropped) {
          return;
        }
        patchCard(card.itemId, { description: full.description ?? "" });
      })
      .catch(() => {
        if (!dropped) {
          setBodyFailed(true);
        }
      });
    return () => {
      dropped = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [card.itemId, bodyLoaded]);

  // The body arriving (or changing under an untouched pane — a teammate
  // edited it) refreshes the draft; a dirty draft is the user's and stays.
  // The editor holds its own document, so a saved text it has not seen is
  // a new editor (the version keys it); while the user types, nothing
  // arriving may remount it out from under the caret.
  const [editorVersion, setEditorVersion] = useState(0);
  const cancelling = useRef(false);
  useEffect(() => {
    if (!dirty && card.description !== undefined && card.description !== description) {
      setDescription(card.description);
      setEditorVersion((v) => v + 1);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [card.description]);

  // The card's full activity feed (events + notes) loads on demand — fetched
  // fresh every time the Activity tab is opened, so the timeline is current.
  useEffect(() => {
    if (tab !== "activity" || temp) {
      return;
    }
    let cancelled = false;
    void provider
      .listLog(card.itemId)
      .then((l) => {
        if (!cancelled) {
          setLog(l);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setLog({ notes: [], events: [] });
        }
      });
    return () => {
      cancelled = true;
    };
    // log deliberately not a dep: refetch is keyed to opening the tab.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab, card.itemId, provider]);

  // Timeline grouped by day, newest day first (entries inside stay in order).
  const timeline = useMemo(() => {
    if (!log) {
      return [];
    }
    type Entry = { at: string; note?: Note; event?: CardEvent };
    const entries: Entry[] = [
      ...log.notes.map((n) => ({ at: n.createdAt, note: n })),
      ...log.events.map((e) => ({ at: e.at, event: e })),
    ];
    entries.sort((a, b) => a.at.localeCompare(b.at));
    const days = new Map<string, Entry[]>();
    for (const e of entries) {
      const day = localDateIso(e.at) || "—";
      const list = days.get(day) ?? [];
      list.push(e);
      days.set(day, list);
    }
    return [...days.entries()].sort((a, b) => b[0].localeCompare(a[0]));
  }, [log]);

  const commitTitle = () => {
    const next = title.trim();
    setEditingTitle(false);
    if (!next || next === card.title) {
      setTitle(card.title);
      return;
    }
    patchCard(card.itemId, { title: next });
    void provider.patchCard(card.itemId, { title: next }).catch((err: unknown) => {
      patchCard(card.itemId, { title: card.title });
      setTitle(card.title);
      onError?.(errMessage(err));
    });
  };

  const saveDescription = () => {
    const next = description;
    // The description live-syncs with the linked counterpart (original <->
    // review card) server-side; mirror it locally, since our own watch echo
    // is suppressed. Notes stay per-card.
    const counterpart = board.cards.find((c) =>
      card.reviewOf ? c.itemId === card.reviewOf : c.reviewOf === card.itemId,
    );
    // Apply at once and keep the pane open; the write runs behind it. The
    // stale server-derived linkRefs drop so the row falls back to parsing
    // the new text — the links icon follows the edit. A failed write
    // restores exactly what was there, including a counterpart body that
    // was never loaded (undefined stays undefined — never invented as "").
    patchCard(card.itemId, { description: next, linkRefs: undefined });
    if (counterpart) {
      patchCard(counterpart.itemId, { description: next, linkRefs: undefined });
    }
    setDirty(false);
    void provider.patchCard(card.itemId, { description: next }).catch((err: unknown) => {
      patchCard(card.itemId, { description: card.description, linkRefs: card.linkRefs });
      if (counterpart) {
        patchCard(counterpart.itemId, { description: counterpart.description, linkRefs: counterpart.linkRefs });
      }
      onError?.(errMessage(err));
    });
  };

  const time = (at: string) =>
    new Date(at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false });
  const status = statusOf(card);
  const stageLabel = card.stage && card.stage !== "done" ? STAGES[card.stage].label : null;

  const row = (label: string, value: ReactNode, key = label) => (
    <div className="drawer-row" key={key}>
      <span className="drawer-label">{label}</span>
      <span className="drawer-value">{value}</span>
    </div>
  );

  return (
    <div
      className="drawer-backdrop"
      onClick={onClose}
      ref={backdropRef}
      tabIndex={-1}
      onKeyDown={(e) => {
        if (drawerEscapes(e, editingTitle || !!choosing)) {
          e.stopPropagation();
          onClose();
        }
      }}
    >
      <aside className="drawer card-drawer" role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()}>
        <header className="drawer-head">
          {/* Where this card stands in the hierarchy, root first: the goal,
              the initiative, the epic — and for a subtask, its task. A
              planning crumb leads to the Roadmap, which is the one board that
              draws it; a task crumb is a name, since the day boards are where
              a task is met. */}
          {(onBack || crumbs.length > 0) && (
            <nav className="modal-crumbs drawer-crumbs" aria-label="Hierarchy">
              {onBack && (
                <button type="button" className="drawer-back" onClick={onBack} title="Back to the card this was opened from">
                  ‹ Back
                </button>
              )}
              {/* Every crumb opens that card here; a planning crumb also
                  offers the Roadmap, which is the one board that draws it. */}
              {crumbs.map((up, i) => (
                <span key={up.uid} className="modal-crumb">
                  {i > 0 && (
                    <span className="modal-crumb-sep" aria-hidden="true">
                      ›
                    </span>
                  )}
                  <KindIcon kind={(up.type ?? "task") as RoadmapKind} />
                  {onOpenCard ? (
                    <button
                      type="button"
                      className="modal-crumb-link"
                      title={`Open «${up.title}»`}
                      onClick={() => openById(up.uid)}
                    >
                      {up.title}
                    </button>
                  ) : (
                    <span className="modal-crumb-name">{up.title}</span>
                  )}
                  {up.type && onReveal && (
                    <button
                      type="button"
                      className="modal-crumb-reveal"
                      title={`Show «${up.title}» on the Roadmap`}
                      aria-label={`Show ${up.title} on the Roadmap`}
                      onClick={() => onReveal(up.uid)}
                    >
                      ↗
                    </button>
                  )}
                </span>
              ))}
            </nav>
          )}
          <div className="drawer-title-row">
            <KindIcon kind={kind} size={18} className="drawer-kind" label />
            {editingTitle && !isRecord ? (
              <input
                type="text"
                className="modal-title-input drawer-title-input"
                autoFocus
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    commitTitle();
                  } else if (e.key === "Escape") {
                    setTitle(card.title);
                    setEditingTitle(false);
                  }
                }}
                onBlur={commitTitle}
              />
            ) : (
              <h2
                className={isRecord ? "drawer-title" : "drawer-title modal-title-click"}
                onClick={() => {
                  if (isRecord) {
                    return;
                  }
                  setTitle(card.title);
                  setEditingTitle(true);
                }}
                title={isRecord ? undefined : "Click to rename"}
              >
                {card.title}
                {!isRecord && (
                  <span className="modal-title-edit" aria-hidden="true">
                    ✎
                  </span>
                )}
              </h2>
            )}
            {/* Copy ID and Copy link sit left of the ✕. Keyed on the card, so
                a "copied" mark never carries over. */}
            {cardLinkFor && <CardIdentity key={card.itemId} uid={card.itemId} link={cardLinkFor(card.itemId)} />}
            <button type="button" className="modal-close" onClick={onClose} aria-label="Close">
              ✕
            </button>
          </div>
        </header>

        <div className="drawer-body">
          <main className="drawer-main">
            {/* The tabs stand at the top of the main column, as wide as it:
                they switch what the column shows, and a strip that spanned
                the whole drawer read as a second header. */}
            <div className="drawer-tabs-row">
              <div className="modal-tabs drawer-tabs" role="tablist" aria-label="Card sections">
                <button
                  type="button"
                  role="tab"
                  aria-selected={tab === "details"}
                  className={`modal-tab${tab === "details" ? " modal-tab-on" : ""}`}
                  onClick={() => setTab("details")}
                >
                  Details
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={tab === "activity"}
                  className={`modal-tab${tab === "activity" ? " modal-tab-on" : ""}`}
                  onClick={() => setTab("activity")}
                >
                  Activity
                </button>
              </div>
              {isRecord && <span className="drawer-record">A record of {card.asOf}: read only</span>}
            </div>
            {tab === "details" && (
              <>
                <div className="drawer-section-title">Description</div>
                {/* Markdown, edited in place: a click puts the caret where it
                    lands, the syntax renders as it is typed, the clipboard
                    carries Markdown. Keyed on the saved text's version so a
                    body arriving opens a fresh editor and a draft is never
                    remounted away (DescriptionEditor falls back to a box). */}
                <div className="drawer-desc">
                  <DescriptionEditor
                    key={`${card.itemId}:${editorVersion}`}
                    draft={description}
                    value={card.description ?? ""}
                    pending={!bodyLoaded && !temp}
                    readOnly={isRecord}
                    placeholder={
                      bodyLoaded || temp
                        ? "Add a description…"
                        : bodyFailed
                          ? "Failed to load the description — close and reopen to retry."
                          : "Loading the description…"
                    }
                    onChange={(md) => {
                      setDirty(md !== (card.description ?? ""));
                      setDescription(md);
                    }}
                    onSave={() => {
                      if (dirty && (bodyLoaded || temp)) {
                        saveDescription();
                      }
                    }}
                    onBlur={() => {
                      // Leaving the editor saves the draft — unless the
                      // focus is leaving for the Cancel button, which took
                      // the pointer first and says so.
                      if (cancelling.current) {
                        return;
                      }
                      if (dirty && (bodyLoaded || temp) && !isRecord) {
                        saveDescription();
                      }
                    }}
                  />
                </div>
                {!isRecord && (
                  <div className="drawer-desc-actions">
                    {dirty && (
                      <button
                        type="button"
                        className="btn"
                        onPointerDown={() => {
                          cancelling.current = true;
                        }}
                        onClick={() => {
                          cancelling.current = false;
                          setDescription(card.description ?? "");
                          setDirty(false);
                          setEditorVersion((v) => v + 1);
                        }}
                      >
                        Cancel
                      </button>
                    )}
                    <button
                      type="button"
                      className="btn btn-primary"
                      onClick={saveDescription}
                      // Saving over a body that never arrived would write ""
                      // over the real text for everyone.
                      disabled={!dirty || (!bodyLoaded && !temp)}
                    >
                      Save
                    </button>
                  </div>
                )}

                {/* What this card waits for, and what waits for it
                    (dependencies.md) — above what sits under it. A review
                    card neither waits nor is waited for. */}
                {!card.reviewOf && (
                  <DependsOn
                    card={card}
                    known={known}
                    options={around}
                    readOnly={isRecord || temp}
                    onWantOptions={wantDependencyOptions}
                    onAdd={(t) => setDependsOn(withDependency(card.dependsOn, t.itemId))}
                    onRemove={(uid) => setDependsOn(withoutDependency(card.dependsOn, uid))}
                    onOpen={openById}
                  />
                )}

                {/* A task's subtasks: what rides it, each opening here, each
                    with the × that asks the Team board's question (ungroup,
                    or delete), and one more typed in — grouped under this
                    task the way the boards group a card. */}
                {task && !subtask && (
                  <section className="drawer-children" aria-label="Subtasks">
                    <div className="drawer-section-title">
                      Subtasks
                      {subtasks.length > 0 && <span className="modal-level-count"> {subtasks.length}</span>}
                    </div>
                    {subtasks.map((c) => (
                      <div key={c.itemId} className="drawer-child">
                        <button
                          type="button"
                          className="drawer-child-open"
                          onClick={() => onOpenCard?.(c)}
                          disabled={!onOpenCard}
                          title={c.title}
                        >
                          <KindIcon kind="subtask" />
                          <span className="drawer-child-title">{c.title}</span>
                          <span className={`roadmap-status roadmap-status-${statusOf(c)}`}>{STATUS_LABEL[statusOf(c)]}</span>
                        </button>
                        {!isRecord && (
                          <button
                            type="button"
                            className="drawer-child-x"
                            title={`Delete «${c.title}», or take it out of the group`}
                            aria-label={`Remove ${c.title}`}
                            onClick={() => setChoosing({ card: c, choices: ["delete", "ungroup"] })}
                          >
                            ×
                          </button>
                        )}
                      </div>
                    ))}
                    {pendingSubs.map((t) => (
                      <div key={`pending-${t}`} className="drawer-child drawer-child-pending">
                        <span className="drawer-child-title">{t}</span>
                        <span className="drawer-hint">adding…</span>
                      </div>
                    ))}
                    {!isRecord &&
                      !temp &&
                      (addingSub ? (
                        <form
                          className="drawer-add"
                          onSubmit={(e) => {
                            e.preventDefault();
                            addSubtask();
                          }}
                        >
                          <input
                            type="text"
                            className="drawer-add-input"
                            autoFocus
                            value={subTitle}
                            placeholder={`New subtask of «${card.title}»…`}
                            onChange={(e) => setSubTitle(e.target.value)}
                            onKeyDown={(e) => {
                              if (e.key === "Escape") {
                                setAddingSub(false);
                                setSubTitle("");
                              }
                            }}
                          />
                          <button type="submit" className="btn btn-primary" disabled={!subTitle.trim()}>
                            Add
                          </button>
                        </form>
                      ) : (
                        <button type="button" className="drawer-add-open" onClick={() => setAddingSub(true)}>
                          + Add subtask
                        </button>
                      ))}
                  </section>
                )}

                {/* What sits under a planning card, as the Roadmap groups it:
                    the levels below, the tasks. A row opens
                    that card here; its × asks the Team board's question. */}
                {planning && (
                  <section className="drawer-children" aria-label="Under this card">
                    <div className="drawer-section-title">
                      Under it
                      {card.hierarchy && card.hierarchy.total > 0 && (
                        <span className="modal-level-count"> {card.hierarchy.done}/{card.hierarchy.total} tasks done</span>
                      )}
                    </div>
                    {around === null && !temp && <p className="drawer-hint">Loading what sits under it…</p>}
                    {around !== null && !hasChildren && pendingAdds.length === 0 && (
                      <p className="drawer-hint">Nothing under it yet.</p>
                    )}
                    <PlanTree
                      cards={known}
                      root={card}
                      onOpen={(c) => onOpenCard?.(c)}
                      onCreate={addChild}
                      onRemove={(c) => setChoosing({ card: c, choices: hierarchyChoices(c) })}
                      pending={pendingAdds}
                      avatars={avatars}
                      names={names}
                      readOnly={isRecord || temp}
                    />
                  </section>
                )}
              </>
            )}

            {tab === "activity" && (
              <div className="modal-log drawer-log">
                {log === null && <p className="notes-empty">Loading…</p>}
                {log !== null && timeline.length === 0 && <p className="notes-empty">No activity yet.</p>}
                {timeline.map(([day, entries]) => (
                  <div className="modal-log-day" key={day}>
                    <div className="modal-log-date">{day}</div>
                    {entries.map((e) =>
                      e.event ? (
                        <div className="modal-log-row modal-log-event" key={e.event.id}>
                          <span className="modal-log-time">{time(e.at)}</span>
                          {e.event.actor ? `@${e.event.actor} · ` : ""}
                          {eventLabel(e.event)}
                        </div>
                      ) : (
                        <div className="modal-log-row" key={e.note?.id}>
                          <span className="modal-log-time">{time(e.at)}</span>
                          {e.note?.author ? `@${e.note.author} · ` : ""}
                          {e.note?.body}
                        </div>
                      ),
                    )}
                  </div>
                ))}
                {/* The server's clone reaches only so far back: what is shown is
                    everything it has, not everything that happened. */}
                {log?.truncatedBefore && (
                  <p className="notes-empty modal-log-notice">
                    Older history before {localDateIso(log.truncatedBefore)} is not loaded.
                  </p>
                )}
              </div>
            )}
          </main>

          <aside className="drawer-side">
            {/* The rows are the card's kind's: a planning card has a level and
                a standing; a task a band, a size, its people. What the server
                refuses is not offered. */}
            {planning &&
              row(
                "Level",
                <>
                  <KindIcon kind={kind} />
                  {KIND_LABEL[kind]}
                </>,
              )}
            {row(
              "Status",
              <>
                <span className={`roadmap-status roadmap-status-${status}`}>{STATUS_LABEL[status]}</span>
                {planning && card.hierarchy && card.hierarchy.total > 0 && (
                  <span className="modal-level-count" title="Tasks done, of those under it">
                    {card.hierarchy.done}/{card.hierarchy.total}
                  </span>
                )}
                {!planning && stageLabel && <span className="drawer-chip">{stageLabel}</span>}
              </>,
            )}
            {/* The bar a task carries on its board, with its handle: drag it
                here as there. A planning card's progress is derived and has
                no bar of its own. */}
            {!planning &&
              row(
                "Progress",
                <div className="drawer-progress">
                  <ProgressSlider
                    large
                    value={progressValue}
                    locked={clampsProgress(card.stage)}
                    fill={barColor(doneish ? "done" : card.stage)}
                    readOnly={isRecord}
                    onCommit={setProgress}
                  />
                  <span className="drawer-progress-pct">{progressValue}%</span>
                </div>,
              )}
            {/* Whose the task is: its team and the person on it, in the one
                menu the boards ask it with. A subtask's owner is its task's
                (the server cascades it) and only reads here. */}
            {task &&
              row(
                "Owner",
                <span className="drawer-owner-field">
                  {subtask ? (
                  <DrawerOwnerFace owner={owner} inherited={inheritedTeam} avatars={avatars} names={names} title="A subtask's owner is its task's" />
                ) : (
                  <OwnerPicker
                    current={owner}
                    teams={board.teams}
                    people={members}
                    cards={board.cards}
                    avatars={avatars}
                    names={names}
                    disabled={isRecord || temp}
                    onPick={setOwner}
                    className="drawer-owner-picker"
                  >
                    <DrawerOwnerFace owner={owner} inherited={inheritedTeam} avatars={avatars} names={names} />
                    {!isRecord && !temp && <span className="add-card-team-caret">▾</span>}
                  </OwnerPicker>
                  )}
                  {!subtask && !card.reviewOf && !isRecord && !temp && (owner.team || owner.person) && (
                    <OwnerClear onClear={() => setOwner({ team: "", person: "" })} />
                  )}
                </span>,
              )}
            {/* On review: who is reviewing it — the person on the linked
                review card — chosen from those who can read the card's
                repository, as the boards' picker chooses. */}
            {!planning &&
              onReview &&
              row(
                "Reviewer",
                <span className="drawer-picker" ref={reviewerRef}>
                  <button
                    type="button"
                    className={`drawer-pick${reviewer ? "" : " drawer-pick-empty"}`}
                    disabled={isRecord || temp}
                    aria-haspopup="listbox"
                    aria-expanded={reviewerOpen}
                    title="Who reviews it"
                    onClick={() => setReviewerOpen((o) => !o)}
                  >
                    {reviewer && <Avatar login={reviewer} avatars={avatars} names={names} className="avatar-img roadmap-owner-avatar" />}
                    <span className="drawer-pick-text">{reviewer ? displayName(reviewer, names) : "Nobody yet"}</span>
                    {!isRecord && !temp && <span className="add-card-team-caret">▾</span>}
                  </button>
                  <Dropdown open={reviewerOpen} anchorRef={reviewerRef} onClose={() => setReviewerOpen(false)} className="add-card-team-menu drawer-pick-menu">
                    {reviewers.map((p) => (
                      <button
                        key={p}
                        type="button"
                        className={`add-card-team-item${reviewer === p ? " drawer-pick-on" : ""}`}
                        onClick={() => setReviewer(p)}
                      >
                        <Avatar login={p} avatars={avatars} names={names} />
                        {displayName(p, names)}
                      </button>
                    ))}
                    {reviewCard && (
                      <button type="button" className="add-card-team-item drawer-pick-none" onClick={() => setReviewer(null)}>
                        No reviewer
                      </button>
                    )}
                  </Dropdown>
                </span>,
              )}
            {task &&
              row(
                "Zone",
                <span className="modal-zone drawer-zone" ref={zoneRef}>
                  <button
                    type="button"
                    className="drawer-pick modal-zone-btn"
                    disabled={isRecord}
                    onClick={() => setZoneOpen((o) => !o)}
                    title="What kind of work this is"
                  >
                    {card.zone && <span className="team-dot" style={{ background: ZONES[card.zone].accent }} />}
                    <span className="drawer-pick-text">{card.zone ? ZONES[card.zone].spine.toLowerCase() : "none"}</span>
                    {!isRecord && <span className="add-card-team-caret">▾</span>}
                  </button>
                  <Dropdown open={zoneOpen} anchorRef={zoneRef} onClose={() => setZoneOpen(false)} className="add-card-team-menu">
                    {ZONE_ORDER.map((z) => (
                      <button key={z} type="button" className="add-card-team-item" onClick={() => setZone(z)}>
                        <span className="team-dot" style={{ background: ZONES[z].accent }} />
                        {ZONES[z].spine.toLowerCase()}
                      </button>
                    ))}
                  </Dropdown>
                </span>,
              )}
            {task &&
              row(
                "Size",
                <span className="modal-size-chips" role="group" aria-label="Size">
                  {SIZE_ORDER.map((s) => (
                    <button
                      key={s}
                      type="button"
                      className={`modal-size-chip${card.size === s ? " modal-size-chip-on" : ""}`}
                      disabled={isRecord}
                      aria-pressed={card.size === s}
                      title={`${s} — ${SIZES[s].hint} (${SIZES[s].points} pt)`}
                      onClick={() => setSize(s)}
                    >
                      {s}
                    </button>
                  ))}
                </span>,
              )}
            {/* A plan item's team — who carries it out — and its OWNER, the
                one person who answers for it: one picker, where a person
                needs no team (ownership.md, O1, O9). */}
            {planning &&
              row(
                "Owner",
                <span className="drawer-owner-field">
                <OwnerPicker
                  planning
                  current={planOwner}
                  teams={board.teams}
                  people={members}
                  cards={board.cards}
                  avatars={avatars}
                  names={names}
                  disabled={isRecord || temp}
                  onPick={setPlanOwner}
                  className="drawer-owner-picker"
                  title="Who carries it out (a team) and who answers for it (its owner)"
                >
                  <DrawerOwnerFace owner={planOwner} inherited={inheritedTeam} avatars={avatars} names={names} />
                  {!isRecord && !temp && <span className="add-card-team-caret">▾</span>}
                </OwnerPicker>
                {!isRecord && !temp && (planOwner.team || planOwner.person) && (
                  <OwnerClear onClear={() => setPlanOwner({ team: "", person: "" })} />
                )}
                </span>,
              )}
            {editsDates
              ? row(
                  "Dates",
                  <span className="drawer-dates" role="group" aria-label="Dates">
                    <input
                      type="date"
                      value={card.startDate ?? ""}
                      aria-label="Start"
                      onChange={(e) => setDates(e.target.value, card.day ?? "")}
                    />
                    <span className="drawer-dates-sep">→</span>
                    <input
                      type="date"
                      value={card.day ?? ""}
                      aria-label="End"
                      onChange={(e) => setDates(card.startDate ?? "", e.target.value)}
                    />
                  </span>,
                )
              : (card.startDate || card.day) &&
                row(
                  "Dates",
                  <>
                    {card.startDate ?? "…"} → {card.day ?? "…"}
                  </>,
                )}
            {task && card.sprintStart && row("Sprint", card.sprintStart)}
            {subtask &&
              parentCrumb &&
              row(
                "Subtask of",
                onOpenCard ? (
                  <button type="button" className="drawer-pick drawer-pick-link" title={`Open «${parentCrumb.title}»`} onClick={() => openById(parentCrumb.uid)}>
                    <KindIcon kind="task" />
                    <span className="drawer-pick-text">{parentCrumb.title}</span>
                  </button>
                ) : (
                  parentCrumb.title
                ),
              )}
            {/* What this card sits UNDER — the one link of the hierarchy. A
                subtask's parent is its task and is changed by the group
                gesture on the boards; everything else picks a planning card
                of a higher level here, or none. */}
            {offersParent &&
              row(
                "Under",
                <ParentPicker
                  kind={kind}
                  current={parentCrumb}
                  options={around}
                  self={card.itemId}
                  disabled={isRecord || temp}
                  onOpen={() => setWantsOptions(true)}
                  onPick={setParent}
                />,
              )}
            {/* Who the work is for: the card's own customers and those it
                inherits (ownership.md, O4). */}
            {row(
                "Customers",
                <CustomerField
                  chips={customerChips(card, known)}
                  roster={board.customers ?? []}
                  disabled={isRecord || temp}
                  onAdd={(id) => setCustomers([...(card.customers ?? []), id])}
                  onRemove={(id) => setCustomers((card.customers ?? []).filter((x) => x !== id))}
                  onCreate={createCustomer}
                />,
              )}
            {card.domain && board.domains && board.domains.length > 1 && row("Stored in", card.domain)}

            {!isRecord && !temp && (
              <div className="drawer-actions">
                {!planning && (
                  <button type="button" className="drawer-action" onClick={() => setDone(!complete)}>
                    {complete ? "Reopen" : "Mark as done"}
                  </button>
                )}
                <button
                  type="button"
                  className="drawer-action drawer-action-danger"
                  onClick={() => setChoosing({ card, choices: card.parent && !subtask ? hierarchyChoices(card) : ["delete"] })}
                >
                  {card.parent && !subtask ? "Delete or unlink…" : "Delete…"}
                </button>
              </div>
            )}
          </aside>
        </div>
      </aside>

      {choosing && (
        <RemoveChoiceDialog
          title={choosing.card.title}
          progress={
            choosing.card.hierarchy
              ? Math.round((100 * choosing.card.hierarchy.done) / Math.max(choosing.card.hierarchy.total, 1))
              : (choosing.card.progress ?? 0)
          }
          choices={choosing.choices}
          subtasks={0}
          note={childrenNote(known, choosing.card.itemId)}
          onClose={() => setChoosing(null)}
          onSubmit={(choice) => answer(choosing.card, choice)}
        />
      )}
    </div>
  );
}

/** DrawerOwnerFace is what the drawer's Owner shows: the card's own team and
 *  person, or — with no team of its own — the team it counts under (O3),
 *  drawn translucent as the Roadmap's Owner cell draws it, with where it
 *  comes from in the tooltip. The picker around it still sets the card's OWN
 *  value. */
function DrawerOwnerFace({
  owner,
  inherited,
  avatars,
  names,
  title,
}: {
  owner: Owner;
  inherited: { team: string; from: string } | null;
  avatars?: Avatars;
  names?: Names;
  title?: string;
}) {
  const borrowed = !owner.team && inherited ? inherited : null;
  const shown: Owner = borrowed ? { ...owner, team: borrowed.team } : owner;
  const label = ownerLabel(shown, (l) => displayName(l, names));
  const tip = borrowed ? `${label} — ${borrowed.from ? `from «${borrowed.from}»` : "inherited"}` : title;
  const cls = `drawer-owner${shown.team || shown.person ? "" : " drawer-owner-none"}${borrowed ? " drawer-owner-inherited" : ""}`;
  return (
    <span className={cls} title={tip}>
      {shown.team && <span className="team-dot drawer-owner-team" style={{ background: teamColor(shown.team) }} />}
      {shown.person && <Avatar login={shown.person} avatars={avatars} names={names} className="avatar-img roadmap-owner-avatar" />}
      <span className="drawer-owner-text">{shown.team || shown.person ? label : "Nobody yet"}</span>
    </span>
  );
}

/** OwnerClear is the drawer Owner's ×: what the picker's "No owner" does —
 *  the card's own team and person off — in one press, shown only while the
 *  card has either of its own (an inherited team is nothing to unset). */
function OwnerClear({ onClear }: { onClear: () => void }) {
  return (
    <button type="button" className="drawer-owner-clear" aria-label="Unset the owner" title="Unset the owner" onClick={onClear}>
      ×
    </button>
  );
}
