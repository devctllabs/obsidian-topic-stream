import { useMutation } from "@tanstack/react-query";
import { useEffect } from "react";
import type { Schema } from "../../api/contracts";
import { ActionMenu } from "../../shared/components/ActionMenu";
import type { SaveIssue } from "./use-snapshot-workflow";

export function IssueSnapshotAction({ issue, save }: { issue: Schema<"DigestIssue">; save: SaveIssue }) {
  const operation = useMutation({ mutationFn: (anchor: HTMLElement) => save(issue, anchor), retry: false });
  useEffect(() => { if (operation.isSuccess) operation.variables.focus({ preventScroll: true }); }, [operation.isSuccess, operation.variables]);
  const result = operation.data;
  return <div className="topic-stream-issue-save"><ActionMenu label="Issue actions" menuLabel="Issue actions" disabled={operation.isPending} items={[{ id: "save", label: "Save to vault", onSelect: (anchor) => operation.mutate(anchor) }]} />
    {result && <p role="status" className="topic-stream-metadata">{result.localImages ? `Saved to vault with ${result.localImages} local images.` : "Saved to vault."}{result.linkedImages > 0 && ` ${result.linkedImages} images kept as links.`}</p>}
  </div>;
}
