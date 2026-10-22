import type { Belt, BeltSnapshot, BeltsResponse } from "../../api/wire.gen";

/** A saved belt the editor opens. The daemon omits `belt` when the stored definition is the draft, so the draft stands in for it. */
export type BeltRecord = BeltSnapshot & { belt: Belt };

/** The belt list with every record's stored definition filled in. */
export interface BeltList extends Omit<BeltsResponse, "belts"> {
  belts: BeltRecord[];
}

export function beltRecord(snapshot: BeltSnapshot): BeltRecord {
  return { ...snapshot, belt: snapshot.belt ?? snapshot.draft };
}

export function beltList(response: BeltsResponse): BeltList {
  return { ...response, belts: response.belts.map(beltRecord) };
}
