import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import {
  useAuthKeys,
  useRotateAuthKey,
  usePruneAuthKeys,
} from "@/api/hooks";
import type { AuthKey } from "@/api/types";
import { Badge, Button, Modal, Input, Spinner, EmptyState } from "@/components/common";

const DEFAULT_OVERLAP_SECONDS = 300;
const FEEDBACK_TIMEOUT_MS = 4000;

// shortKeyId — the backend ids are random base64ish; full id is mostly
// noise to a human eye, the first 8 chars are enough to disambiguate
// while still being copyable from the row.
function shortKeyId(id: string): string {
  return id.length > 12 ? `${id.slice(0, 8)}…${id.slice(-4)}` : id;
}

function formatDateTime(iso: string): string {
  if (!iso) return "—";
  return new Date(iso).toLocaleString();
}

/**
 * AuthKeysPanel — admin surface for the JWT signing-key lifecycle.
 *
 * The /admin/auth/keys backend has been around for a while (see
 * router.go) but had no UI; this panel closes that gap.
 *
 *   - List shows every key with its primary/retired state and
 *     creation/retirement timestamps.
 *   - "Rotar clave" mints a new primary with a configurable overlap
 *     window. The default of 5 minutes matches the backend's safe
 *     default and is what an admin should pick for any non-emergency
 *     rotation. Setting overlap to 0 retires the previous primary
 *     immediately — that's the post-leak path and the modal calls
 *     it out.
 *   - "Purgar retiradas" removes keys whose retirement is in the past;
 *     it is non-destructive in any meaningful sense (retired keys
 *     can't sign or validate any active session) but is grouped at
 *     the bottom so the eye lands on rotation first.
 */
export function AuthKeysPanel() {
  const { t } = useTranslation();
  const { data: keys, isLoading, error } = useAuthKeys();
  const rotate = useRotateAuthKey();
  const prune = usePruneAuthKeys();

  const [showRotate, setShowRotate] = useState(false);
  const [overlapInput, setOverlapInput] = useState(String(DEFAULT_OVERLAP_SECONDS));
  const [feedback, setFeedback] = useState<{
    type: "success" | "error";
    text: string;
  } | null>(null);

  // Auto-clear feedback so a stale "rotated key xyz" doesn't loiter on
  // the panel after the user moves on. 4s is consistent with the
  // libraries refresh banner.
  useEffect(() => {
    if (!feedback) return;
    const id = setTimeout(() => setFeedback(null), FEEDBACK_TIMEOUT_MS);
    return () => clearTimeout(id);
  }, [feedback]);

  const handleRotate = () => {
    const overlap = Number.parseInt(overlapInput, 10);
    rotate.mutate(
      { overlapSeconds: Number.isFinite(overlap) ? Math.max(0, overlap) : DEFAULT_OVERLAP_SECONDS },
      {
        onSuccess: (data) => {
          setShowRotate(false);
          setFeedback({
            type: "success",
            text: t("admin.authKeys.rotateSuccess", { id: shortKeyId(data.id) }),
          });
        },
        onError: () =>
          setFeedback({ type: "error", text: t("admin.authKeys.rotateFailed") }),
      },
    );
  };

  const handlePrune = () => {
    prune.mutate(undefined, {
      onSuccess: (data) =>
        setFeedback({
          type: "success",
          text: t("admin.authKeys.pruneSuccess", { count: data.pruned }),
        }),
      onError: () =>
        setFeedback({ type: "error", text: t("admin.authKeys.pruneFailed") }),
    });
  };

  return (
    <section className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-lg font-semibold text-text-primary">
            {t("admin.authKeys.title")}
          </h3>
          <p className="mt-1 max-w-2xl text-sm text-text-secondary">
            {t("admin.authKeys.description")}
          </p>
        </div>
        <div className="flex gap-2">
          <Button
            variant="secondary"
            size="sm"
            onClick={handlePrune}
            isLoading={prune.isPending}
          >
            {prune.isPending ? t("admin.authKeys.pruning") : t("admin.authKeys.prune")}
          </Button>
          <Button size="sm" onClick={() => setShowRotate(true)}>
            {t("admin.authKeys.rotate")}
          </Button>
        </div>
      </div>

      {feedback && (
        <div
          className={[
            "rounded-[--radius-md] px-4 py-2 text-sm",
            feedback.type === "success"
              ? "bg-success/10 text-success"
              : "bg-error/10 text-error",
          ].join(" ")}
        >
          {feedback.text}
        </div>
      )}

      {isLoading ? (
        <div className="flex justify-center py-8">
          <Spinner size="md" />
        </div>
      ) : error ? (
        <EmptyState
          title={t("admin.authKeys.loadFailed")}
          description={error.message}
        />
      ) : !keys || keys.length === 0 ? (
        <EmptyState title={t("admin.authKeys.noKeys")} />
      ) : (
        <KeysTable keys={keys} />
      )}

      <Modal
        isOpen={showRotate}
        onClose={() => setShowRotate(false)}
        title={t("admin.authKeys.rotateConfirmTitle")}
        size="md"
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-text-secondary">
            {t("admin.authKeys.rotateConfirmBody")}
          </p>
          <Input
            label={t("admin.authKeys.overlapSeconds")}
            type="number"
            inputMode="numeric"
            min={0}
            value={overlapInput}
            onChange={(e) => setOverlapInput(e.target.value)}
          />
          <p className="-mt-2 text-[11px] text-text-muted">
            {t("admin.authKeys.overlapHint")}
          </p>
          {rotate.error && (
            <p className="text-xs text-error">{rotate.error.message}</p>
          )}
          <div className="flex justify-end gap-3 pt-2">
            <Button
              variant="secondary"
              type="button"
              onClick={() => setShowRotate(false)}
            >
              {t("common.cancel")}
            </Button>
            <Button
              variant="danger"
              onClick={handleRotate}
              isLoading={rotate.isPending}
            >
              {rotate.isPending ? t("admin.authKeys.rotating") : t("admin.authKeys.rotate")}
            </Button>
          </div>
        </div>
      </Modal>
    </section>
  );
}

function KeysTable({ keys }: { keys: AuthKey[] }) {
  const { t } = useTranslation();

  // Sort: primary first, then by created_at desc. Snapshot is unordered
  // from the backend's perspective so we impose a stable ordering here
  // — admins always want to see the active key at the top.
  const sorted = [...keys].sort((a, b) => {
    if (a.is_primary !== b.is_primary) return a.is_primary ? -1 : 1;
    return b.created_at.localeCompare(a.created_at);
  });

  return (
    <div className="overflow-x-auto rounded-[--radius-lg] border border-border">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-border bg-bg-elevated text-left text-text-muted">
            <th className="px-4 py-3 font-medium">{t("admin.authKeys.id")}</th>
            <th className="px-4 py-3 font-medium">{t("admin.authKeys.primary")}</th>
            <th className="px-4 py-3 font-medium">{t("admin.authKeys.createdAt")}</th>
            <th className="px-4 py-3 font-medium">{t("admin.authKeys.retiredAt")}</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {sorted.map((key) => (
            <tr key={key.id} className="bg-bg-card">
              <td className="px-4 py-3 font-mono text-xs text-text-primary" title={key.id}>
                {shortKeyId(key.id)}
              </td>
              <td className="px-4 py-3">
                {key.is_primary ? (
                  <Badge variant="success">{t("admin.authKeys.primary")}</Badge>
                ) : (
                  <Badge>{t("admin.authKeys.retired")}</Badge>
                )}
              </td>
              <td className="px-4 py-3 text-text-secondary tabular-nums">
                {formatDateTime(key.created_at)}
              </td>
              <td className="px-4 py-3 text-text-secondary tabular-nums">
                {key.retired_at ? formatDateTime(key.retired_at) : "—"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
