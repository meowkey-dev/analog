import { useMemo } from "react";
import type { AnalogEvent, Node } from "./api";

/**
 * The event log, reverse-chronologically (SPEC §5).
 *
 * Runs of the same actor and event type inside a short window collapse into one
 * line, so a bulk add_cards reads as "claude-code added 3 cards", not eight rows.
 * Clicking a group pans the canvas to its subject.
 */

const COALESCE_MS = 60_000;

// verb, plural noun, and what one of them is called when there is no subject to name.
const VERBS: Record<string, [string, string, string]> = {
  "card.created": ["added", "cards", "a card"],
  "card.updated": ["edited", "cards", "a card"],
  "card.moved": ["moved", "cards", "a card"],
  "card.deleted": ["deleted", "cards", "a card"],
  "link.created": ["linked", "cards", "a link"],
  "link.deleted": ["removed", "links", "a link"],
  "annotation.created": ["commented on", "cards", "a card"],
  "annotation.resolved": ["resolved", "comments", "a comment"],
};

export interface Group {
  key: string;
  actor: string;
  actorKind: string;
  type: string;
  events: AnalogEvent[];
  ts: string;
}

export function groupEvents(events: AnalogEvent[]): Group[] {
  const ascending = [...events].sort((a, b) => a.seq - b.seq);
  const groups: Group[] = [];
  for (const event of ascending) {
    const last = groups[groups.length - 1];
    const withinWindow =
      last &&
      last.actor === event.actor &&
      last.type === event.type &&
      Math.abs(Date.parse(event.ts) - Date.parse(last.events[last.events.length - 1]!.ts)) <= COALESCE_MS;
    if (withinWindow) {
      last.events.push(event);
      last.ts = event.ts;
    } else {
      groups.push({
        key: `${event.actor}:${event.type}:${event.seq}`,
        actor: event.actor,
        actorKind: event.actor_kind,
        type: event.type,
        events: [event],
        ts: event.ts,
      });
    }
  }
  return groups.reverse();
}

function describe(group: Group, titleOf: (id: string) => string): string {
  const [verb, noun, one] = VERBS[group.type] ?? ["changed", "things", "something"];
  const count = group.events.length;
  if (count === 1) {
    const event = group.events[0]!;
    const subject =
      group.type.startsWith("annotation")
        ? titleOf(event.payload?.card_id ?? "")
        : group.type.startsWith("link")
          ? [titleOf(event.payload?.from ?? ""), titleOf(event.payload?.to ?? "")].filter(Boolean).join(" → ")
          : titleOf(event.subject_id) || event.payload?.title || "";
    // Older events may carry no payload, leaving nothing to name.
    return subject ? `${verb} ${subject}` : `${verb} ${one}`;
  }
  return `${verb} ${count} ${noun}`;
}

function relative(ts: string): string {
  const seconds = (Date.now() - Date.parse(ts)) / 1000;
  if (seconds < 60) return "just now";
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86_400) return `${Math.floor(seconds / 3600)}h ago`;
  return new Date(ts).toLocaleDateString();
}

export function Activity(props: {
  events: AnalogEvent[];
  nodes: Node[];
  onFocus: (subjectId: string) => void;
}) {
  const titles = useMemo(() => {
    const map = new Map<string, string>();
    for (const node of props.nodes) map.set(node.id, node.sp_title || node.id);
    return map;
  }, [props.nodes]);

  const titleOf = (id: string) => titles.get(id) ?? "";
  const groups = useMemo(() => groupEvents(props.events), [props.events]);

  return (
    <div className="panel activity-panel">
      <header><h2>Activity</h2></header>
      {groups.length === 0 && <p className="empty">Nothing has happened yet.</p>}
      <ul>
        {groups.map((group) => {
          const first = group.events[0]!;
          const target = group.type.startsWith("annotation")
            ? (first.payload?.card_id ?? first.subject_id)
            : group.type.startsWith("link")
              ? (first.payload?.from ?? first.subject_id)
              : first.subject_id;
          return (
            <li key={group.key} className="activity-row" onClick={() => props.onFocus(target)}>
              <span className={`actor ${group.actorKind}`}>
                {group.actorKind === "human" ? "you" : group.actor}
              </span>
              <span className="what">{describe(group, titleOf)}</span>
              <time dateTime={group.ts} title={new Date(group.ts).toLocaleString()}>
                {relative(group.ts)}
              </time>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
