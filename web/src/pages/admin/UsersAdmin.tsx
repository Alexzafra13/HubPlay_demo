import { useEffect, useMemo, useState } from "react";
import type { FormEvent, ReactNode } from "react";
import type { User } from "@/api/types";
import {
  useUsers,
  useCreateUser,
  useCreatePersonalIPTVLibrary,
  useCreateProfile,
  useDeleteUser,
  useLibraries,
  useMe,
  useResetUserPassword,
  useSetUserAccess,
  useSetUserActive,
  useSetUserAvatarColor,
  useSetUserContentRating,
  useSetUserDisplayName,
  useSetUserLibraryAccess,
  useSetUserPIN,
  useSetUserRole,
  useUserLibraryAccess,
} from "@/api/hooks";
import { Button, KebabMenu, Modal, Input, EmptyState, Skeleton } from "@/components/common";
import type { KebabMenuItem } from "@/components/common";
import { AVATAR_PALETTE, avatarColorForUser } from "@/utils/avatarColor";
import { useIsMobile } from "@/hooks/useIsMobile";
import { getInitials } from "@/utils/userDisplay";
import {
  ChevronDown,
  ChevronRight,
  KeyRound,
  Library as LibraryIcon,
  Lock,
  Palette,
  Tv,
  Trash2,
  UserPlus,
} from "lucide-react";
import { Trans, useTranslation } from 'react-i18next';
import FederationAdmin from "./FederationAdmin";
import { LibraryAccessCheckboxes } from "./LibraryAccessCheckboxes";

export default function UsersAdmin() {
  const { t } = useTranslation();
  const isMobile = useIsMobile();
  const { data: me } = useMe();
  const { data: users, isLoading, error } = useUsers();
  const createUser = useCreateUser();
  const deleteUser = useDeleteUser();

  const [showAddModal, setShowAddModal] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<User | null>(null);
  const [resetTarget, setResetTarget] = useState<User | null>(null);

  // Set of parent IDs whose profile members are currently expanded.
  // Default collapsed so a parent with members reads as a single line
  // ("alex · 2 miembros · ▸") and admins drill in on demand. Parents
  // with zero members render plain — no chevron, no badge — so a
  // typical single-account install isn't littered with empty
  // affordances.
  const [expandedParents, setExpandedParents] = useState<Set<string>>(new Set());

  function toggleParent(parentId: string) {
    setExpandedParents((prev) => {
      const next = new Set(prev);
      if (next.has(parentId)) next.delete(parentId);
      else next.add(parentId);
      return next;
    });
  }

  // Add user form state. autoGenerate defaults true so the admin's
  // happy path is "type username, click Create, copy the password
  // the modal shows" — no thinking required for normal use.
  const [newUsername, setNewUsername] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [autoGenerate, setAutoGenerate] = useState(true);
  const [newDisplayName, setNewDisplayName] = useState("");
  const [newRole, setNewRole] = useState("user");
  // Optional pre-checked library grants applied in the same POST.
  // Pre-load all libraries by default so the happy-path "create user
  // with full household access" is one click — the admin un-ticks if
  // they want to restrict, not the other way round.
  const [newGrantLibraryIds, setNewGrantLibraryIds] = useState<string[]>([]);

  // Library list — used by both the create modal (grant checkboxes)
  // and the edit-access modal (matrix view). Cached behind useQuery,
  // so reusing the same hook in two places shares cache.
  const { data: libraries } = useLibraries();
  const allLibraries = useMemo(() => libraries ?? [], [libraries]);

  // Modal that surfaces a one-shot password to copy. Drives both the
  // post-create flow and the post-reset flow — same UX (a chip with
  // Copy + a "share with the user" hint), different trigger.
  const [generatedPasswordModal, setGeneratedPasswordModal] = useState<{
    username: string;
    password: string;
    kind: "created" | "reset";
  } | null>(null);

  const resetPassword = useResetUserPassword();
  const createProfile = useCreateProfile();
  const setUserPIN = useSetUserPIN();
  const setUserContentRating = useSetUserContentRating();
  const setUserDisplayName = useSetUserDisplayName();
  const setUserAvatarColor = useSetUserAvatarColor();

  // Personalize modal — admin (or parent of profile, or self) edits
  // both the display_name AND the avatar colour override here. We
  // keep both controls in one place because the operator's mental
  // model is "edit this profile" rather than "rename" + "recolour"
  // as separate actions. On submit, only the dirty fields fire
  // their respective mutations.
  const [renameTarget, setRenameTarget] = useState<User | null>(null);
  const [renameValue, setRenameValue] = useState("");
  const [renameColor, setRenameColor] = useState<string>("");
  const [renameError, setRenameError] = useState<string | null>(null);

  function openRename(user: User) {
    setRenameTarget(user);
    setRenameValue(user.display_name || user.username.split("/").pop() || "");
    setRenameColor(user.avatar_color ?? "");
    setRenameError(null);
  }

  function handleRename(e: FormEvent) {
    e.preventDefault();
    if (!renameTarget) return;
    const nextName = renameValue.trim();
    if (!nextName) {
      setRenameError(t("admin.users.renameValidation", {
        defaultValue: "El nombre no puede estar vacío.",
      }));
      return;
    }

    const nameDirty = nextName !== renameTarget.display_name;
    const colorDirty = renameColor !== (renameTarget.avatar_color ?? "");

    if (!nameDirty && !colorDirty) {
      // Nothing changed; just close.
      setRenameTarget(null);
      return;
    }

    // Fire both mutations in parallel when both are dirty. Each
    // invalidates the same queryKeys; TanStack Query will collapse
    // the refetches.
    const tasks: Promise<unknown>[] = [];
    if (nameDirty) {
      tasks.push(
        setUserDisplayName.mutateAsync({
          userId: renameTarget.id,
          displayName: nextName,
        }),
      );
    }
    if (colorDirty) {
      tasks.push(
        setUserAvatarColor.mutateAsync({
          userId: renameTarget.id,
          hex: renameColor,
        }),
      );
    }
    Promise.all(tasks)
      .then(() => {
        setRenameTarget(null);
        setRenameValue("");
        setRenameColor("");
        setRenameError(null);
      })
      .catch((err: Error) => setRenameError(err.message));
  }
  const setUserRole = useSetUserRole();
  const setUserActive = useSetUserActive();
  const setUserAccess = useSetUserAccess();

  // "Add profile" modal — admin types a display name; the server
  // synthesises the username + a throwaway password (profiles can't
  // log in directly anyway).
  const [profileParent, setProfileParent] = useState<User | null>(null);
  const [profileName, setProfileName] = useState("");

  // Edit-library-access modal. `accessTarget` is the row the kebab was
  // clicked from; the modal's GET fetches the canonical owner (parent
  // for profiles) and we mirror its `library_ids` into local state so
  // the admin can stage changes before hitting Save. Closing without
  // saving discards the local edits.
  const [accessTarget, setAccessTarget] = useState<User | null>(null);
  const [accessDraft, setAccessDraft] = useState<string[] | null>(null);
  const setUserLibraryAccess = useSetUserLibraryAccess();
  const { data: accessData, isLoading: accessLoading } = useUserLibraryAccess(
    accessTarget?.id,
  );
  useEffect(() => {
    // Seed the draft once when the server response lands. If the admin
    // already edited the draft, leave it alone — re-seeding would
    // discard their in-progress changes when the query refetches.
    if (accessData && accessDraft === null) {
      setAccessDraft(accessData.library_ids);
    }
  }, [accessData, accessDraft]);

  function closeAccessModal() {
    setAccessTarget(null);
    setAccessDraft(null);
    setUserLibraryAccess.reset();
  }

  function handleSaveAccess() {
    if (!accessTarget || !accessData || accessDraft === null) return;
    setUserLibraryAccess.mutate(
      {
        // ALWAYS hit the owner id (parent for profiles). The server
        // would reject a profile target with 400; we resolve here
        // proactively so the admin can edit a profile's matrix
        // through the same kebab — the GET already normalises and the
        // PUT honours that contract.
        userId: accessData.owner_id,
        libraryIds: accessDraft,
      },
      {
        onSuccess: () => closeAccessModal(),
      },
    );
  }

  // PIN modal — admin types a 4-digit PIN (or clears it).
  const [pinTarget, setPinTarget] = useState<User | null>(null);
  const [pinValue, setPinValue] = useState("");
  const [pinError, setPinError] = useState<string | null>(null);

  // Personal IPTV modal — admin types a name + M3U URL and the
  // backend creates the library + grant in one transaction. The
  // default name is seeded from the target's display name so the
  // admin doesn't have to re-type "Lista de Juan" by hand.
  const [iptvTarget, setIptvTarget] = useState<User | null>(null);
  const [iptvName, setIptvName] = useState("");
  const [iptvM3U, setIptvM3U] = useState("");
  const [iptvEPG, setIptvEPG] = useState("");
  const [iptvTLSInsecure, setIptvTLSInsecure] = useState(false);
  const [iptvError, setIptvError] = useState<string | null>(null);
  const createPersonalIPTV = useCreatePersonalIPTVLibrary();
  function openIptvModal(user: User) {
    setIptvTarget(user);
    const displayName = user.display_name || user.username;
    setIptvName(
      t("admin.users.iptvPersonalDefaultName", {
        defaultValue: "Lista de {{name}}",
        name: displayName,
      }),
    );
    setIptvM3U("");
    setIptvEPG("");
    setIptvTLSInsecure(false);
    setIptvError(null);
    createPersonalIPTV.reset();
  }
  function closeIptvModal() {
    setIptvTarget(null);
    createPersonalIPTV.reset();
  }
  function handleCreatePersonalIPTV(e: FormEvent) {
    e.preventDefault();
    if (!iptvTarget) return;
    const name = iptvName.trim();
    const m3uUrl = iptvM3U.trim();
    if (!name) {
      setIptvError(
        t("admin.users.iptvPersonalNameRequired", {
          defaultValue: "Indica un nombre para la lista.",
        }),
      );
      return;
    }
    if (!m3uUrl) {
      setIptvError(
        t("admin.users.iptvPersonalM3URequired", {
          defaultValue: "Indica la URL del M3U.",
        }),
      );
      return;
    }
    setIptvError(null);
    createPersonalIPTV.mutate(
      {
        userId: iptvTarget.id,
        name,
        m3uUrl,
        epgUrl: iptvEPG.trim() || undefined,
        tlsInsecure: iptvTLSInsecure || undefined,
      },
      { onSuccess: () => closeIptvModal() },
    );
  }

  function handleCreateProfile(e: FormEvent) {
    e.preventDefault();
    if (!profileParent || !profileName.trim()) return;
    createProfile.mutate(
      {
        parentUserId: profileParent.id,
        displayName: profileName.trim(),
      },
      {
        onSuccess: () => {
          setProfileParent(null);
          setProfileName("");
        },
      },
    );
  }

  // Age-friendly labels for the content-rating dropdown. The
  // backend's ranking table maps both MPAA literals (G/PG/PG-13/R/
  // NC-17) and US-TV literals (TV-Y/Y7/G/PG/14/MA) to the same five
  // tiers, so the admin only needs to pick one of six rungs (Sin
  // límite + 5 ages). The stored value is the MPAA literal that
  // anchors that tier; the rating filter still catches TV-* content
  // at the equivalent rung because it queries the rank, not the
  // literal. Modelled after how Disney+ / Netflix surface "+13"
  // rather than the raw MPAA / TV codes.
  const ratingOptions: { value: string; key: string; defaultLabel: string }[] = [
    { value: "",      key: "admin.users.ratingNone",      defaultLabel: "Sin límite (+18)" },
    { value: "G",     key: "admin.users.ratingAllAges",   defaultLabel: "Apto para todos" },
    { value: "PG",    key: "admin.users.rating7",         defaultLabel: "+7" },
    { value: "PG-13", key: "admin.users.rating13",        defaultLabel: "+13" },
    { value: "R",     key: "admin.users.rating17",        defaultLabel: "+17" },
  ];

  // When the stored cap is a TV-* literal (a legacy / federated row,
  // or a manually edited DB value), the dropdown wouldn't match any
  // of the five MPAA-anchored rungs. Map back to the equivalent MPAA
  // anchor so the select renders a valid current-value highlight.
  // NC-17 / TV-MA collapse to "" (Sin límite) — an adult cap is
  // semantically the same as no restriction in this UI, so we don't
  // expose two options that do effectively the same thing.
  function ratingDropdownValue(rating: string | undefined): string {
    if (!rating) return "";
    const tvToMpaa: Record<string, string> = {
      "TV-Y": "G", "TV-G": "G",
      "TV-Y7": "PG", "TV-PG": "PG",
      "TV-14": "PG-13",
      "TV-MA": "",
      "NC-17": "",
    };
    return tvToMpaa[rating] ?? rating;
  }

  // Maps the dropdown selection to a number of days for the
  // /users/{id}/access endpoint. 0 = clear → permanent. The select
  // also lets admins read the current state ("Caduca en 5 días")
  // without exposing the raw timestamp.
  const accessOptions: { value: number; key: string; defaultLabel: string }[] = [
    { value: 0, key: "admin.users.accessPermanent", defaultLabel: "Permanente" },
    { value: 1, key: "admin.users.access1d", defaultLabel: "1 día" },
    { value: 3, key: "admin.users.access3d", defaultLabel: "3 días" },
    { value: 7, key: "admin.users.access1w", defaultLabel: "1 semana" },
    { value: 30, key: "admin.users.access1m", defaultLabel: "1 mes" },
    { value: 90, key: "admin.users.access3m", defaultLabel: "3 meses" },
    { value: 365, key: "admin.users.access1y", defaultLabel: "1 año" },
  ];

  function describeAccess(user: User): string {
    if (!user.access_expires_at) {
      return t("admin.users.accessPermanent", { defaultValue: "Permanente" });
    }
    const expires = new Date(user.access_expires_at);
    const now = new Date();
    const diffDays = Math.ceil((expires.getTime() - now.getTime()) / 86_400_000);
    if (diffDays <= 0) {
      return t("admin.users.accessExpired", { defaultValue: "Caducado" });
    }
    return t("admin.users.accessExpiresIn", {
      defaultValue: "Caduca en {{days}} días",
      days: diffDays,
    });
  }

  function handleRoleChange(user: User, nextRole: "user" | "admin") {
    if (user.role === nextRole) return;
    // Promotion to admin is irreversible-ish (the new admin gains
    // every dangerous button) so confirm explicitly. Demotion is
    // safe — we still confirm because admins shouldn't lose the
    // role accidentally either, but with softer copy.
    const promptKey =
      nextRole === "admin"
        ? "admin.users.confirmPromote"
        : "admin.users.confirmDemote";
    const ok = window.confirm(
      t(promptKey, {
        defaultValue:
          nextRole === "admin"
            ? "Vas a hacer admin a {{name}}. Tendrá acceso completo al panel. ¿Continuar?"
            : "Vas a quitar permisos de admin a {{name}}. ¿Continuar?",
        name: user.display_name || user.username,
      }),
    );
    if (!ok) return;
    setUserRole.mutate({ userId: user.id, role: nextRole });
  }

  function handleSavePin(e: FormEvent) {
    e.preventDefault();
    if (!pinTarget) return;
    setPinError(null);
    if (pinValue !== "" && !/^\d{4}$/.test(pinValue)) {
      setPinError(t("admin.users.pinValidation", { defaultValue: "El PIN debe ser exactamente 4 dígitos." }));
      return;
    }
    setUserPIN.mutate(
      { userId: pinTarget.id, pin: pinValue },
      {
        onSuccess: () => {
          setPinTarget(null);
          setPinValue("");
        },
        onError: (err) => setPinError(err.message),
      },
    );
  }

  function resetForm() {
    setNewUsername("");
    setNewPassword("");
    setAutoGenerate(true);
    setNewDisplayName("");
    setNewRole("user");
    setNewGrantLibraryIds([]);
  }

  // Default the grant checkboxes to "all libraries" the first time the
  // modal opens after libraries finish loading. The admin's typical
  // intent on a fresh hubplay is "give the new household access to
  // everything"; un-ticking is the deliberate restrict gesture, not
  // the default. We only seed when the modal is open AND the local
  // state is still the initial empty array, so an admin who explicitly
  // un-checks everything before submitting doesn't get auto-re-checked.
  useEffect(() => {
    if (!showAddModal) return;
    if (newGrantLibraryIds.length > 0) return;
    if (allLibraries.length === 0) return;
    setNewGrantLibraryIds(allLibraries.map((l) => l.id));
    // We deliberately omit newGrantLibraryIds from deps: this seed is a
    // ONE-shot when the modal opens, not a reactive sync, otherwise
    // un-ticking everything would immediately re-tick everything.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [showAddModal, allLibraries]);

  function handleCreate(e: FormEvent) {
    e.preventDefault();
    if (!newUsername.trim()) return;
    if (!autoGenerate && !newPassword.trim()) return;

    const username = newUsername.trim();
    createUser.mutate(
      {
        username,
        // Empty `password` triggers the server's auto-generation path
        // — see the AuthHandler.Register handler. The response then
        // carries `generated_password` which we surface to the admin
        // exactly once via the GeneratedPassword modal below.
        password: autoGenerate ? undefined : newPassword,
        display_name: newDisplayName.trim() || undefined,
        role: newRole,
        // Omit when empty so the server's "no grants" branch fires
        // cleanly; sending [] would also work but it's noise.
        grant_library_ids:
          newGrantLibraryIds.length > 0 ? newGrantLibraryIds : undefined,
      },
      {
        onSuccess: (resp) => {
          setShowAddModal(false);
          resetForm();
          if (resp.generated_password) {
            setGeneratedPasswordModal({
              username,
              password: resp.generated_password,
              kind: "created",
            });
          }
        },
      },
    );
  }

  function handleReset() {
    if (!resetTarget) return;
    const target = resetTarget;
    resetPassword.mutate(target.id, {
      onSuccess: (resp) => {
        setResetTarget(null);
        setGeneratedPasswordModal({
          username: target.username,
          password: resp.generated_password,
          kind: "reset",
        });
      },
    });
  }

  function handleDelete() {
    if (!deleteTarget) return;
    deleteUser.mutate(deleteTarget.id, {
      onSuccess: () => setDeleteTarget(null),
    });
  }

  // Action descriptors shared between the desktop row's button
  // strip and the mobile card's kebab menu. Single source of truth
  // so a flag added here lights up in both places automatically.
  // The `hidden` flag is used by the kebab; desktop iterates and
  // skips them with the same predicate.
  function getUserActions(user: User): KebabMenuItem[] {
    const isSelf = me?.id === user.id;
    const isProfile = !!user.parent_user_id;
    return [
      {
        label: t("admin.users.rename", { defaultValue: "Personalizar" }),
        icon: Palette,
        onClick: () => openRename(user),
        hint: t("admin.users.renameHint", {
          defaultValue: "Editar el nombre y el color del avatar",
        }),
      },
      {
        label: t("admin.users.addProfile", { defaultValue: "+ Perfil" }),
        icon: UserPlus,
        onClick: () => setProfileParent(user),
        hidden: isProfile,
        hint: t("admin.users.addProfileHint", {
          defaultValue: "Crear perfil hijo bajo esta cuenta",
        }),
      },
      {
        label: user.has_pin
          ? t("admin.users.pinChange", { defaultValue: "Cambiar PIN" })
          : t("admin.users.pinSetCta", { defaultValue: "Poner PIN" }),
        icon: KeyRound,
        onClick: () => {
          setPinTarget(user);
          setPinValue("");
          setPinError(null);
        },
        hint: t("admin.users.pinHint", {
          defaultValue: "Configurar PIN del perfil",
        }),
      },
      {
        label: t("admin.users.libraryAccessAction", {
          defaultValue: "Bibliotecas",
        }),
        icon: LibraryIcon,
        onClick: () => {
          setAccessTarget(user);
          setAccessDraft(null);
        },
        hint: isProfile
          ? t("admin.users.libraryAccessProfileHint", {
              defaultValue:
                "Editar las bibliotecas del hogar (hereda del titular).",
            })
          : t("admin.users.libraryAccessHint", {
              defaultValue: "Qué bibliotecas ve este hogar.",
            }),
      },
      {
        label: t("admin.users.iptvPersonalAction", {
          defaultValue: "Lista IPTV personal",
        }),
        icon: Tv,
        onClick: () => openIptvModal(user),
        hidden: isProfile,
        hint: t("admin.users.iptvPersonalHint", {
          defaultValue:
            "Crea una biblioteca livetv visible solo para este usuario.",
        }),
      },
      {
        label: t("admin.users.resetPassword", {
          defaultValue: "Reiniciar contraseña",
        }),
        onClick: () => setResetTarget(user),
        hidden: isProfile || isSelf || !!user.is_primary,
        hint: t("admin.users.resetPasswordHint", {
          defaultValue: "Generar contraseña temporal nueva",
        }),
      },
      {
        label: t("common.delete"),
        icon: Trash2,
        danger: true,
        disabled: isSelf || !!user.is_primary,
        hint: user.is_primary
          ? t("admin.users.primaryDeleteHint", {
              defaultValue: "La cuenta principal no se puede eliminar.",
            })
          : undefined,
        onClick: () => setDeleteTarget(user),
      },
    ];
  }

  function formatDate(iso: string) {
    return new Date(iso).toLocaleDateString(undefined, {
      year: "numeric",
      month: "short",
      day: "numeric",
    });
  }

  if (error) {
    return (
      <EmptyState
        title={t('admin.users.failedToLoad')}
        description={t('common.loadErrorHint')}
      />
    );
  }

  // ─── Mobile users list ─────────────────────────────────────────
  //
  // Renders the same parent-with-collapsible-children tree as the
  // desktop table but as stacked cards: avatar + name + tags
  // header, metadata as a 2-col grid, actions behind a kebab menu.
  // Closure-scoped (lives inside UsersAdmin) so it can reach the
  // same handler set the table renderer uses without prop-drilling
  // a dozen mutations through.
  function renderMobileUserList() {
    if (!users) return null;

    const childrenByParent = new Map<string, User[]>();
    const parents: User[] = [];
    for (const u of users) {
      if (u.parent_user_id) {
        const arr = childrenByParent.get(u.parent_user_id) ?? [];
        arr.push(u);
        childrenByParent.set(u.parent_user_id, arr);
      } else {
        parents.push(u);
      }
    }

    const renderCard = (
      user: User,
      opts: {
        expandable?: boolean;
        expanded?: boolean;
        memberCount?: number;
        onToggle?: () => void;
        inGroup?: boolean;
      },
    ) => {
      const isSelf = me?.id === user.id;
      const isProfile = !!user.parent_user_id;
      const palette = avatarColorForUser(user);
      const initials = getInitials({
        display_name: user.display_name,
        username: user.username,
      });
      const username = isProfile
        ? user.username.split("/").pop() ?? user.username
        : user.username;

      return (
        <li
          key={user.id}
          className={[
            "flex flex-col gap-3 rounded-[--radius-lg] border bg-bg-card p-4",
            opts.inGroup
              ? "border-l-2 border-l-accent/50 border-border bg-bg-elevated/60"
              : "border-border",
          ].join(" ")}
        >
          {/* Header row */}
          <div className="flex items-start gap-3">
            {opts.expandable && opts.onToggle && (
              <button
                type="button"
                onClick={opts.onToggle}
                aria-expanded={opts.expanded}
                aria-label={
                  opts.expanded
                    ? t("admin.users.collapseMembers", {
                        defaultValue: "Ocultar miembros",
                      })
                    : t("admin.users.expandMembers", {
                        defaultValue: "Mostrar miembros",
                      })
                }
                className="mt-1.5 inline-flex h-5 w-5 shrink-0 items-center justify-center rounded text-text-muted hover:bg-bg-elevated hover:text-text-primary transition-colors"
              >
                {opts.expanded ? (
                  <ChevronDown className="h-3.5 w-3.5" />
                ) : (
                  <ChevronRight className="h-3.5 w-3.5" />
                )}
              </button>
            )}
            <div
              className="relative flex h-12 w-12 shrink-0 items-center justify-center overflow-hidden rounded-full text-base font-semibold text-white shadow-sm"
              style={{ background: palette.background }}
              aria-hidden
            >
              {initials}
              {user.has_pin && (
                <span
                  className="absolute -bottom-0.5 -right-0.5 flex h-4 w-4 items-center justify-center rounded-full bg-black/70 text-white shadow ring-1 ring-bg-card"
                  aria-label={t("admin.users.pinSet", {
                    defaultValue: "PIN configurado",
                  })}
                >
                  <Lock className="h-2.5 w-2.5" />
                </span>
              )}
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="font-medium text-text-primary truncate">
                  {username}
                </span>
                {isSelf && (
                  <span className="text-xs text-text-muted">
                    {t("admin.users.you")}
                  </span>
                )}
                {user.is_primary && (
                  <span className="rounded-full bg-warning/15 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-warning">
                    {t("admin.users.primaryTag", { defaultValue: "Principal" })}
                  </span>
                )}
                {isProfile && (
                  <span className="text-[10px] uppercase tracking-wider text-accent">
                    {t("admin.users.profileTag", { defaultValue: "perfil" })}
                  </span>
                )}
                {opts.memberCount !== undefined && opts.memberCount > 0 && (
                  <span className="rounded-full border border-border-subtle bg-bg-elevated px-2 py-0.5 text-[10px] font-medium text-text-secondary">
                    {opts.memberCount === 1
                      ? t("admin.users.memberCountOne", {
                          defaultValue: "1 miembro",
                        })
                      : t("admin.users.memberCountOther", {
                          defaultValue: "{{count}} miembros",
                          count: opts.memberCount,
                        })}
                  </span>
                )}
              </div>
              {user.display_name && user.display_name !== username && (
                <p className="mt-0.5 truncate text-xs text-text-muted">
                  {user.display_name}
                </p>
              )}
            </div>
            <KebabMenu
              items={getUserActions(user)}
              ariaLabel={t("admin.users.actions")}
            />
          </div>

          {/* Metadata grid */}
          <dl className="grid grid-cols-2 gap-x-3 gap-y-3 text-xs">
            {/* Rol */}
            <div className="flex flex-col gap-1">
              <dt className="text-text-muted">{t("admin.users.role")}</dt>
              <dd>
                {isProfile ? (
                  <span className="text-text-muted">—</span>
                ) : (
                  <select
                    value={user.role}
                    onChange={(e) =>
                      handleRoleChange(
                        user,
                        e.target.value as "user" | "admin",
                      )
                    }
                    disabled={isSelf || user.is_primary}
                    className="w-full rounded-[--radius-sm] border border-border bg-bg-elevated px-2 py-1 text-xs text-text-primary focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30 disabled:opacity-60 disabled:cursor-not-allowed"
                  >
                    <option value="user">{t("admin.users.roleUser")}</option>
                    <option value="admin">{t("admin.users.roleAdmin")}</option>
                  </select>
                )}
              </dd>
            </div>

            {/* Edad máxima */}
            <div className="flex flex-col gap-1">
              <dt className="text-text-muted">
                {t("admin.users.rating", { defaultValue: "Edad máxima" })}
              </dt>
              <dd>
                {user.role === "admin" ? (
                  <span className="text-text-muted">—</span>
                ) : (
                  <select
                    value={ratingDropdownValue(user.max_content_rating)}
                    onChange={(e) =>
                      setUserContentRating.mutate({
                        userId: user.id,
                        rating: e.target.value,
                      })
                    }
                    className="w-full rounded-[--radius-sm] border border-border bg-bg-elevated px-2 py-1 text-xs text-text-primary focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30"
                  >
                    {ratingOptions.map((opt) => (
                      <option key={opt.value} value={opt.value}>
                        {t(opt.key, { defaultValue: opt.defaultLabel })}
                      </option>
                    ))}
                  </select>
                )}
              </dd>
            </div>

            {/* Estado */}
            <div className="flex flex-col gap-1">
              <dt className="text-text-muted">
                {t("admin.users.status", { defaultValue: "Estado" })}
              </dt>
              <dd>
                {isSelf || user.is_primary || isProfile ? (
                  <span
                    className={
                      user.is_active === false
                        ? "inline-block rounded-full bg-error/15 px-2 py-0.5 text-[11px] font-medium text-error"
                        : "inline-block rounded-full bg-success/15 px-2 py-0.5 text-[11px] font-medium text-success"
                    }
                  >
                    {user.is_active === false
                      ? t("admin.users.statusInactive", {
                          defaultValue: "Inactivo",
                        })
                      : t("admin.users.statusActive", {
                          defaultValue: "Activo",
                        })}
                  </span>
                ) : (
                  <button
                    type="button"
                    onClick={() =>
                      setUserActive.mutate({
                        userId: user.id,
                        isActive: !(user.is_active ?? true),
                      })
                    }
                    className={[
                      "rounded-full px-2 py-0.5 text-[11px] font-medium transition-colors",
                      user.is_active === false
                        ? "bg-error/15 text-error hover:bg-error/25"
                        : "bg-success/15 text-success hover:bg-success/25",
                    ].join(" ")}
                  >
                    {user.is_active === false
                      ? t("admin.users.statusInactive", {
                          defaultValue: "Inactivo",
                        })
                      : t("admin.users.statusActive", {
                          defaultValue: "Activo",
                        })}
                  </button>
                )}
              </dd>
            </div>

            {/* Acceso */}
            <div className="flex flex-col gap-1">
              <dt className="text-text-muted">
                {t("admin.users.access", { defaultValue: "Acceso" })}
              </dt>
              <dd>
                {isProfile || user.is_primary ? (
                  <span className="text-text-muted">—</span>
                ) : (
                  <select
                    value={user.access_expires_at ? -1 : 0}
                    onChange={(e) => {
                      const days = Number(e.target.value);
                      if (days < 0) return;
                      setUserAccess.mutate({
                        userId: user.id,
                        durationDays: days,
                      });
                    }}
                    title={describeAccess(user)}
                    className="w-full rounded-[--radius-sm] border border-border bg-bg-elevated px-2 py-1 text-xs text-text-primary focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30"
                  >
                    {user.access_expires_at && (
                      <option value={-1}>{describeAccess(user)}</option>
                    )}
                    {accessOptions.map((opt) => (
                      <option key={opt.value} value={opt.value}>
                        {t(opt.key, { defaultValue: opt.defaultLabel })}
                      </option>
                    ))}
                  </select>
                )}
              </dd>
            </div>

            {/* Creado — full row */}
            <div className="col-span-2 flex flex-col gap-0.5 border-t border-border-subtle pt-2">
              <dt className="text-text-muted">
                {t("admin.users.created")}
              </dt>
              <dd className="text-text-secondary">{formatDate(user.created_at)}</dd>
            </div>
          </dl>
        </li>
      );
    };

    const cards: ReactNode[] = [];
    for (const parent of parents) {
      const kids = childrenByParent.get(parent.id) ?? [];
      const expanded = expandedParents.has(parent.id);
      cards.push(
        renderCard(parent, {
          expandable: kids.length > 0,
          expanded,
          memberCount: kids.length,
          onToggle: () => toggleParent(parent.id),
          inGroup: expanded && kids.length > 0,
        }),
      );
      if (expanded) {
        for (const kid of kids) {
          cards.push(renderCard(kid, { inGroup: true }));
        }
      }
    }

    return <ul className="flex flex-col gap-3">{cards}</ul>;
  }

  return (
    <div className="flex flex-col gap-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-text-primary">{t('admin.users.title')}</h2>
        <Button onClick={() => setShowAddModal(true)}>{t('admin.users.addUser')}</Button>
      </div>

      {/* Table — render the chrome immediately. While `isLoading`,
          fill the body with skeleton rows that match the real row's
          shape so data arrival doesn't shift layout. */}
      {isLoading ? (
        <div className="overflow-x-auto rounded-[--radius-lg] border border-border">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-bg-elevated text-left text-text-muted">
                <th className="px-4 py-3 font-medium">{t('admin.users.username')}</th>
                <th className="px-4 py-3 font-medium">{t('admin.users.displayName')}</th>
                <th className="px-4 py-3 font-medium">{t('admin.users.role')}</th>
                <th className="px-4 py-3 font-medium">{t('admin.users.rating', { defaultValue: 'Edad máxima' })}</th>
                <th className="px-4 py-3 font-medium">{t('admin.users.access', { defaultValue: 'Acceso' })}</th>
                <th className="px-4 py-3 font-medium">{t('admin.users.status', { defaultValue: 'Estado' })}</th>
                <th className="px-4 py-3 font-medium">{t('admin.users.created')}</th>
                <th className="px-4 py-3 font-medium text-right">{t('admin.users.actions')}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {Array.from({ length: 4 }, (_, i) => (
                <tr key={i} className="bg-bg-card">
                  <td className="px-4 py-3"><Skeleton variant="text" width="60%" /></td>
                  <td className="px-4 py-3"><Skeleton variant="text" width="75%" /></td>
                  <td className="px-4 py-3"><Skeleton variant="rectangular" width={56} height={20} /></td>
                  <td className="px-4 py-3"><Skeleton variant="rectangular" width={70} height={20} /></td>
                  <td className="px-4 py-3"><Skeleton variant="rectangular" width={90} height={20} /></td>
                  <td className="px-4 py-3"><Skeleton variant="rectangular" width={70} height={20} /></td>
                  <td className="px-4 py-3"><Skeleton variant="text" width="55%" /></td>
                  <td className="px-4 py-3">
                    <div className="flex justify-end">
                      <Skeleton variant="rectangular" width={68} height={28} />
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : users && users.length > 0 ? (
        isMobile ? (
          // Mobile: stacked cards instead of an 8-col table.
          // Same parent/child grouping as the desktop branch
          // (parents render with chevron + member count when
          // expandable, kids render under the parent when
          // expanded). Actions live in a kebab menu so the row
          // height stays bounded.
          renderMobileUserList()
        ) : (
        <div className="overflow-x-auto rounded-[--radius-lg] border border-border">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-bg-elevated text-left text-text-muted">
                <th className="px-4 py-3 font-medium">{t('admin.users.username')}</th>
                <th className="px-4 py-3 font-medium">{t('admin.users.displayName')}</th>
                <th className="px-4 py-3 font-medium">{t('admin.users.role')}</th>
                <th className="px-4 py-3 font-medium">{t('admin.users.rating', { defaultValue: 'Edad máxima' })}</th>
                <th className="px-4 py-3 font-medium">{t('admin.users.access', { defaultValue: 'Acceso' })}</th>
                <th className="px-4 py-3 font-medium">{t('admin.users.status', { defaultValue: 'Estado' })}</th>
                <th className="px-4 py-3 font-medium">{t('admin.users.created')}</th>
                <th className="px-4 py-3 font-medium text-right">{t('admin.users.actions')}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {(() => {
                // Group profiles under their parents so the table reads
                // as a flat list of accounts. Parents with members get
                // a chevron + "N miembros" pill that toggles their
                // children's visibility; parents with no members render
                // plain so there's no dead affordance.
                const childrenByParent = new Map<string, User[]>();
                const parents: User[] = [];
                for (const u of users) {
                  if (u.parent_user_id) {
                    const arr = childrenByParent.get(u.parent_user_id) ?? [];
                    arr.push(u);
                    childrenByParent.set(u.parent_user_id, arr);
                  } else {
                    parents.push(u);
                  }
                }

                const renderRow = (
                  user: User,
                  opts: {
                    expandable?: boolean;
                    expanded?: boolean;
                    memberCount?: number;
                    onToggle?: () => void;
                    /** Row sits inside a currently-expanded parent
                     *  group (either the parent itself or one of its
                     *  children). Drives the left accent rail that
                     *  visually ties parent and children together. */
                    inGroup?: boolean;
                  },
                ) => {
                  const isSelf = me?.id === user.id;
                  return (
                  <tr
                    key={user.id}
                    className={[
                      'transition-colors',
                      opts.inGroup
                        ? 'bg-bg-elevated/60 hover:bg-bg-elevated'
                        : 'bg-bg-card hover:bg-bg-elevated',
                    ].join(' ')}
                  >
                    <td
                      className={[
                        'px-4 py-3 font-medium text-text-primary',
                        opts.inGroup
                          ? 'border-l-2 border-accent/50'
                          : '',
                      ].join(' ')}
                    >
                      {/* Chevron only on parent rows that actually
                          have profile members. Click toggles their
                          children's visibility. Parents without
                          members never render the button so there's
                          no inert affordance. */}
                      {opts.expandable && opts.onToggle && (
                        <button
                          type="button"
                          onClick={opts.onToggle}
                          aria-expanded={opts.expanded}
                          aria-label={
                            opts.expanded
                              ? t('admin.users.collapseMembers', {
                                  defaultValue: 'Ocultar miembros',
                                })
                              : t('admin.users.expandMembers', {
                                  defaultValue: 'Mostrar miembros',
                                })
                          }
                          className="mr-2 inline-flex h-5 w-5 items-center justify-center rounded text-text-muted hover:bg-bg-elevated hover:text-text-primary transition-colors"
                        >
                          <svg
                            className={[
                              'h-3.5 w-3.5 transition-transform',
                              opts.expanded ? 'rotate-90' : '',
                            ].join(' ')}
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                            strokeWidth={2.5}
                            strokeLinecap="round"
                            strokeLinejoin="round"
                          >
                            <path d="M9 6l6 6-6 6" />
                          </svg>
                        </button>
                      )}
                      {/* Profiles are visually nested under their
                          parent — easier to read than scanning the
                          parent_user_id column. */}
                      {user.parent_user_id && (
                        <span className="mr-2 text-text-muted" aria-hidden>
                          ↳
                        </span>
                      )}
                      {/* Profile usernames carry the synthetic
                          "<parent>/<name>" prefix the server uses to
                          keep UNIQUE happy; show only the suffix. */}
                      {user.parent_user_id
                        ? user.username.split("/").pop()
                        : user.username}
                      {user.parent_user_id && (
                        <span className="ml-2 text-[10px] uppercase tracking-wider text-accent">
                          {t('admin.users.profileTag', { defaultValue: 'perfil' })}
                        </span>
                      )}
                      {user.has_pin && (
                        <span
                          className="ml-2 inline-flex h-4 w-4 items-center justify-center text-text-muted"
                          aria-label={t('admin.users.pinSet', { defaultValue: 'PIN configurado' })}
                          title={t('admin.users.pinSet', { defaultValue: 'PIN configurado' })}
                        >
                          <svg className="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                            <rect x="5" y="11" width="14" height="9" rx="2" />
                            <path d="M8 11V7a4 4 0 0 1 8 0v4" />
                          </svg>
                        </span>
                      )}
                      {isSelf && (
                        <span className="ml-2 text-xs text-text-muted">
                          {t('admin.users.you')}
                        </span>
                      )}
                      {user.is_primary && (
                        <span
                          className="ml-2 rounded-full bg-warning/15 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-warning"
                          title={t('admin.users.primaryHint', {
                            defaultValue:
                              'Cuenta principal del servidor — no se puede eliminar ni cambiar de rol desde aquí.',
                          })}
                        >
                          {t('admin.users.primaryTag', { defaultValue: 'Principal' })}
                        </span>
                      )}
                      {/* Member count pill — only when this account
                          actually has profile members. Lets the admin
                          read "how many people share this login" at a
                          glance without expanding the row. */}
                      {opts.memberCount !== undefined && opts.memberCount > 0 && (
                        <span className="ml-2 inline-flex items-center gap-1 rounded-full border border-border-subtle bg-bg-elevated px-2 py-0.5 text-[10px] font-medium text-text-secondary">
                          <svg
                            className="h-3 w-3"
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                            strokeWidth={2}
                            strokeLinecap="round"
                            strokeLinejoin="round"
                          >
                            <circle cx="12" cy="8" r="3.5" />
                            <path d="M5 20a7 7 0 0 1 14 0" />
                          </svg>
                          {opts.memberCount === 1
                            ? t('admin.users.memberCountOne', {
                                defaultValue: '1 miembro',
                              })
                            : t('admin.users.memberCountOther', {
                                defaultValue: '{{count}} miembros',
                                count: opts.memberCount,
                              })}
                        </span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-text-secondary">
                      {user.display_name}
                    </td>
                    <td className="px-4 py-3">
                      {/* Role select — disabled on self (no admin
                          can demote themselves accidentally) and on
                          the primary admin row (the bootstrap user
                          is immutable from the UI; recovery happens
                          via DB / setup wizard if ever needed). */}
                      <select
                        value={user.role}
                        onChange={(e) =>
                          handleRoleChange(user, e.target.value as "user" | "admin")
                        }
                        disabled={isSelf || user.is_primary || !!user.parent_user_id}
                        className="rounded-[--radius-sm] border border-border bg-bg-elevated px-2 py-1 text-xs text-text-primary focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30 disabled:opacity-60 disabled:cursor-not-allowed"
                        aria-label={t('admin.users.role')}
                      >
                        <option value="user">{t('admin.users.roleUser')}</option>
                        <option value="admin">{t('admin.users.roleAdmin')}</option>
                      </select>
                    </td>
                    <td className="px-4 py-3">
                      {/* Content cap is meaningless on admin rows
                          (admins see everything) and on the row the
                          admin is logged in as. Hide it instead of
                          leaving a confusing inert select. */}
                      {user.role === "admin" ? (
                        <span className="text-xs text-text-muted">
                          {t('admin.users.ratingNotApplicable', { defaultValue: '—' })}
                        </span>
                      ) : (
                        <select
                          value={ratingDropdownValue(user.max_content_rating)}
                          onChange={(e) =>
                            setUserContentRating.mutate({
                              userId: user.id,
                              rating: e.target.value,
                            })
                          }
                          className="rounded-[--radius-sm] border border-border bg-bg-elevated px-2 py-1 text-xs text-text-primary focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30"
                          aria-label={t('admin.users.rating', {
                            defaultValue: 'Edad máxima',
                          })}
                          title={t('admin.users.ratingHint', {
                            defaultValue:
                              'Cubre los códigos MPAA (G/PG/PG-13/R/NC-17) y de TV (TV-Y/Y7/G/PG/14/MA) del mismo nivel.',
                          })}
                        >
                          {ratingOptions.map((opt) => (
                            <option key={opt.value} value={opt.value}>
                              {t(opt.key, { defaultValue: opt.defaultLabel })}
                            </option>
                          ))}
                        </select>
                      )}
                    </td>
                    <td className="px-4 py-3">
                      {/* Profiles inherit access from their parent, so
                          surfacing the dropdown on profile rows would
                          mislead. Primary admin is locked too. */}
                      {user.parent_user_id || user.is_primary ? (
                        <span className="text-xs text-text-muted">
                          {t('admin.users.accessInherits', { defaultValue: '—' })}
                        </span>
                      ) : (
                        <select
                          value={user.access_expires_at ? -1 : 0}
                          onChange={(e) => {
                            const days = Number(e.target.value);
                            if (days < 0) return; // "Current" placeholder; no-op
                            setUserAccess.mutate({ userId: user.id, durationDays: days });
                          }}
                          className="rounded-[--radius-sm] border border-border bg-bg-elevated px-2 py-1 text-xs text-text-primary focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30"
                          aria-label={t('admin.users.access', { defaultValue: 'Acceso' })}
                          title={describeAccess(user)}
                        >
                          {/* Negative-value placeholder shows the
                              current state when expires_at is set;
                              picking it again is a no-op so accidental
                              selection doesn't mutate. */}
                          {user.access_expires_at && (
                            <option value={-1}>{describeAccess(user)}</option>
                          )}
                          {accessOptions.map((opt) => (
                            <option key={opt.value} value={opt.value}>
                              {t(opt.key, { defaultValue: opt.defaultLabel })}
                            </option>
                          ))}
                        </select>
                      )}
                    </td>
                    <td className="px-4 py-3">
                      {/* Active toggle. Hidden on the row's own user
                          (cannot deactivate self) and on the primary
                          admin (server rejects with 403 PRIMARY_ADMIN_LOCKED
                          anyway). Profiles inherit from parent. */}
                      {isSelf || user.is_primary || user.parent_user_id ? (
                        <span
                          className={
                            user.is_active === false
                              ? 'rounded-full bg-error/15 px-2 py-0.5 text-[11px] font-medium text-error'
                              : 'rounded-full bg-success/15 px-2 py-0.5 text-[11px] font-medium text-success'
                          }
                        >
                          {user.is_active === false
                            ? t('admin.users.statusInactive', { defaultValue: 'Inactivo' })
                            : t('admin.users.statusActive', { defaultValue: 'Activo' })}
                        </span>
                      ) : (
                        <button
                          type="button"
                          onClick={() =>
                            setUserActive.mutate({
                              userId: user.id,
                              isActive: !(user.is_active ?? true),
                            })
                          }
                          className={[
                            'rounded-full px-2 py-0.5 text-[11px] font-medium transition-colors',
                            user.is_active === false
                              ? 'bg-error/15 text-error hover:bg-error/25'
                              : 'bg-success/15 text-success hover:bg-success/25',
                          ].join(' ')}
                          title={
                            user.is_active === false
                              ? t('admin.users.activateHint', { defaultValue: 'Click para reactivar' })
                              : t('admin.users.deactivateHint', { defaultValue: 'Click para desactivar' })
                          }
                        >
                          {user.is_active === false
                            ? t('admin.users.statusInactive', { defaultValue: 'Inactivo' })
                            : t('admin.users.statusActive', { defaultValue: 'Activo' })}
                        </button>
                      )}
                    </td>
                    <td className="px-4 py-3 text-text-secondary">
                      {formatDate(user.created_at)}
                    </td>
                    <td className="px-4 py-3">
                      {/* Desktop reuses the same KebabMenuItem[] the
                          mobile card menu consumes (getUserActions
                          above). `hidden` filters out actions that
                          don't apply to this row (profile vs parent,
                          self, primary admin); `disabled`/`hint`/`danger`
                          map straight onto the Button props. The
                          icons attached to each item are intentionally
                          ignored here — the strip stays text-only to
                          keep table rows compact. */}
                      <div className="flex justify-end gap-2 flex-wrap">
                        {getUserActions(user)
                          .filter((action) => !action.hidden)
                          .map((action) => (
                            <Button
                              key={action.label}
                              variant={action.danger ? 'danger' : 'secondary'}
                              size="sm"
                              disabled={action.disabled}
                              onClick={action.onClick}
                              title={action.hint}
                            >
                              {action.label}
                            </Button>
                          ))}
                      </div>
                    </td>
                  </tr>
                  );
                };

                const rows: ReactNode[] = [];
                for (const parent of parents) {
                  const kids = childrenByParent.get(parent.id) ?? [];
                  const expanded = expandedParents.has(parent.id);
                  rows.push(
                    renderRow(parent, {
                      expandable: kids.length > 0,
                      expanded,
                      memberCount: kids.length,
                      onToggle: () => toggleParent(parent.id),
                      inGroup: expanded && kids.length > 0,
                    }),
                  );
                  if (expanded) {
                    for (const kid of kids) {
                      rows.push(renderRow(kid, { inGroup: true }));
                    }
                  }
                }
                return rows;
              })()}
            </tbody>
          </table>
        </div>
        )
      ) : (
        <EmptyState
          title={t('admin.users.noUsers')}
          description={t('admin.users.noUsersHint')}
        />
      )}

      {/* Add User Modal */}
      <Modal
        isOpen={showAddModal}
        onClose={() => setShowAddModal(false)}
        title={t('admin.users.addUser')}
      >
        <form onSubmit={handleCreate} className="flex flex-col gap-4">
          <Input
            label={t('admin.users.username')}
            placeholder={t('admin.users.usernamePlaceholder')}
            value={newUsername}
            onChange={(e) => setNewUsername(e.target.value)}
            required
          />

          {/* Auto-generate is the default. The server picks a
              readable temporary password and returns it once in the
              response; the GeneratedPasswordModal below surfaces it
              to the admin to share with the user. The user is forced
              to rotate at first login. */}
          <label className="flex items-start gap-2 text-sm text-text-secondary cursor-pointer select-none">
            <input
              type="checkbox"
              className="mt-0.5"
              checked={autoGenerate}
              onChange={(e) => setAutoGenerate(e.target.checked)}
            />
            <span>
              <span className="block font-medium text-text-primary">
                {t('admin.users.autoGeneratePassword', { defaultValue: 'Generar contraseña automáticamente' })}
              </span>
              <span className="block text-xs text-text-muted">
                {t('admin.users.autoGenerateHint', {
                  defaultValue:
                    'Te enseñaremos la contraseña una sola vez. El usuario tendrá que cambiarla al iniciar sesión.',
                })}
              </span>
            </span>
          </label>

          {!autoGenerate && (
            <Input
              label={t('admin.users.password')}
              type="password"
              placeholder={t('admin.users.passwordPlaceholder')}
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              required
              minLength={8}
            />
          )}

          <Input
            label={t('admin.users.displayName')}
            placeholder={t('admin.users.displayNamePlaceholder')}
            value={newDisplayName}
            onChange={(e) => setNewDisplayName(e.target.value)}
          />

          <div className="flex flex-col gap-1.5">
            <label
              htmlFor="user-role"
              className="text-sm font-medium text-text-secondary"
            >
              {t('admin.users.role')}
            </label>
            <select
              id="user-role"
              value={newRole}
              onChange={(e) => setNewRole(e.target.value)}
              className="w-full rounded-[--radius-md] bg-bg-card border border-border px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/30"
            >
              <option value="user">{t('admin.users.roleUser')}</option>
              <option value="admin">{t('admin.users.roleAdmin')}</option>
            </select>
          </div>

          <div className="flex flex-col gap-1.5">
            <label className="text-sm font-medium text-text-secondary">
              {t("admin.users.libraryAccessSectionLabel", {
                defaultValue: "Bibliotecas accesibles",
              })}
            </label>
            <p className="text-xs text-text-muted">
              {t("admin.users.libraryAccessSectionHint", {
                defaultValue:
                  "Por defecto, el nuevo usuario ve todas las bibliotecas existentes. Desmarca las que no quieras que vea.",
              })}
            </p>
            <LibraryAccessCheckboxes
              libraries={allLibraries}
              selectedIds={newGrantLibraryIds}
              onChange={setNewGrantLibraryIds}
            />
          </div>

          {createUser.error && (
            <p className="text-xs text-error">{createUser.error.message}</p>
          )}

          <div className="flex justify-end gap-3 pt-2">
            <Button
              variant="secondary"
              type="button"
              onClick={() => setShowAddModal(false)}
            >
              {t('common.cancel')}
            </Button>
            <Button type="submit" isLoading={createUser.isPending}>
              {t('common.create')}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Edit Library Access modal. Loads the canonical grant set via
          GET (which normalises profile→parent server-side), keeps a
          local draft so the admin can stage changes, and persists with
          PUT against the OWNER id. Profile rows render the same
          editable matrix because grants apply to the household, but
          the header makes clear which top-level account is being
          edited so the operator isn't surprised when changing one
          profile's row affects everyone under that parent. */}
      <Modal
        isOpen={accessTarget !== null}
        onClose={closeAccessModal}
        title={t("admin.users.libraryAccessModalTitle", {
          defaultValue: "Bibliotecas accesibles",
        })}
      >
        <div className="flex flex-col gap-4">
          {accessLoading || !accessData ? (
            <Skeleton className="h-32 w-full" />
          ) : (
            <>
              {accessData.is_inherited && (
                <p className="text-xs text-text-muted">
                  <Trans
                    i18nKey="admin.users.libraryAccessInheritedNotice"
                    defaults="Editas el acceso del titular del hogar <strong>{{owner}}</strong>. Todos los perfiles bajo esa cuenta heredan el mismo set."
                    values={{
                      owner:
                        users?.find((u) => u.id === accessData.owner_id)
                          ?.display_name ??
                        users?.find((u) => u.id === accessData.owner_id)
                          ?.username ??
                        accessData.owner_id,
                    }}
                    components={{ strong: <strong className="text-text-primary" /> }}
                  />
                </p>
              )}
              <LibraryAccessCheckboxes
                libraries={allLibraries}
                selectedIds={accessDraft ?? []}
                onChange={setAccessDraft}
              />
              {setUserLibraryAccess.error && (
                <p className="text-xs text-error">
                  {setUserLibraryAccess.error.message}
                </p>
              )}
              <div className="flex justify-end gap-3">
                <Button variant="secondary" onClick={closeAccessModal}>
                  {t("common.cancel")}
                </Button>
                <Button
                  onClick={handleSaveAccess}
                  isLoading={setUserLibraryAccess.isPending}
                  disabled={accessDraft === null}
                >
                  {t("common.save")}
                </Button>
              </div>
            </>
          )}
        </div>
      </Modal>

      {/* Personal IPTV list modal. Backend POST creates the library
          AND the lone library_access grant in one tx so the operator
          never sees an orphan public library mid-flow. After save we
          refetch the matrix-access cache for this user — the new
          library will show ticked the next time they open the access
          modal. */}
      <Modal
        isOpen={iptvTarget !== null}
        onClose={closeIptvModal}
        title={t("admin.users.iptvPersonalModalTitle", {
          defaultValue: "Lista IPTV personal",
        })}
      >
        <form
          onSubmit={handleCreatePersonalIPTV}
          className="flex flex-col gap-4"
        >
          <p className="text-xs text-text-muted">
            <Trans
              i18nKey="admin.users.iptvPersonalModalHint"
              defaults="Solo <strong>{{name}}</strong> verá esta lista. Para todos los demás, sigue siendo invisible."
              values={{
                name:
                  iptvTarget?.display_name ||
                  iptvTarget?.username ||
                  "",
              }}
              components={{ strong: <strong className="text-text-primary" /> }}
            />
          </p>
          <Input
            label={t("admin.users.iptvPersonalName", {
              defaultValue: "Nombre",
            })}
            placeholder={t("admin.users.iptvPersonalNamePlaceholder", {
              defaultValue: "Lista de Juan",
            })}
            value={iptvName}
            onChange={(e) => {
              setIptvName(e.target.value);
              if (iptvError) setIptvError(null);
            }}
            required
          />
          <Input
            label={t("admin.users.iptvPersonalM3U", {
              defaultValue: "URL M3U",
            })}
            placeholder="https://ejemplo.com/playlist.m3u"
            value={iptvM3U}
            onChange={(e) => {
              setIptvM3U(e.target.value);
              if (iptvError) setIptvError(null);
            }}
            required
          />
          <div className="flex flex-col gap-1">
            <Input
              label={t("admin.users.iptvPersonalEPG", {
                defaultValue: "URL EPG (opcional)",
              })}
              placeholder="https://ejemplo.com/epg.xml"
              value={iptvEPG}
              onChange={(e) => setIptvEPG(e.target.value)}
            />
            <p className="text-[11px] leading-snug text-text-muted">
              {t("admin.users.iptvPersonalEPGHint", {
                defaultValue:
                  "Si el M3U trae url-tvg en su cabecera, se auto-detecta.",
              })}
            </p>
          </div>
          <label className="flex items-start gap-2 text-xs text-text-muted">
            <input
              type="checkbox"
              checked={iptvTLSInsecure}
              onChange={(e) => setIptvTLSInsecure(e.target.checked)}
              className="mt-0.5 accent-accent"
            />
            <span>
              {t("admin.users.iptvPersonalTLSInsecure", {
                defaultValue:
                  "TLS inseguro (omitir verificación de certificado). Úsalo solo si el proveedor lo requiere.",
              })}
            </span>
          </label>

          {iptvError && (
            <p
              role="alert"
              className="rounded-[--radius-sm] border border-error/40 bg-error-soft px-3 py-2 text-xs text-error"
            >
              {iptvError}
            </p>
          )}
          {createPersonalIPTV.error && (
            <p className="text-xs text-error">
              {createPersonalIPTV.error.message}
            </p>
          )}

          <div className="flex justify-end gap-3 pt-1">
            <Button variant="secondary" type="button" onClick={closeIptvModal}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" isLoading={createPersonalIPTV.isPending}>
              {t("admin.users.iptvPersonalSubmit", {
                defaultValue: "Crear lista",
              })}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Delete Confirmation Modal */}
      <Modal
        isOpen={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title={t('admin.users.deleteUser')}
        size="sm"
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-text-secondary">
            {/* See LibrariesAdmin for the rationale — embedded <strong>
                in the i18n string needs <Trans> to render as real
                markup instead of literal "<strong/>" text. */}
            <Trans
              i18nKey="admin.users.deleteConfirm"
              values={{ name: deleteTarget?.username ?? "" }}
              components={{ strong: <strong className="text-text-primary" /> }}
            />
          </p>

          {deleteUser.error && (
            <p className="text-xs text-error">{deleteUser.error.message}</p>
          )}

          <div className="flex justify-end gap-3">
            <Button
              variant="secondary"
              onClick={() => setDeleteTarget(null)}
            >
              {t('common.cancel')}
            </Button>
            <Button
              variant="danger"
              isLoading={deleteUser.isPending}
              onClick={handleDelete}
            >
              {t('common.delete')}
            </Button>
          </div>
        </div>
      </Modal>

      {/* Reset password confirm. Two-click action because it
          invalidates every active session for the target — the user
          will be kicked out of every device and forced to enter the
          new temp password on next login. */}
      <Modal
        isOpen={resetTarget !== null}
        onClose={() => setResetTarget(null)}
        title={t('admin.users.resetPassword', { defaultValue: 'Reiniciar contraseña' })}
        size="sm"
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-text-secondary">
            <Trans
              i18nKey="admin.users.resetConfirm"
              values={{ name: resetTarget?.username ?? '' }}
              defaults="Se generará una contraseña temporal para <strong>{{name}}</strong>. Sus sesiones activas se cerrarán y tendrá que cambiar la contraseña al volver a entrar."
              components={{ strong: <strong className="text-text-primary" /> }}
            />
          </p>
          {resetPassword.error && (
            <p className="text-xs text-error">{resetPassword.error.message}</p>
          )}
          <div className="flex justify-end gap-3">
            <Button variant="secondary" onClick={() => setResetTarget(null)}>
              {t('common.cancel')}
            </Button>
            <Button
              variant="danger"
              isLoading={resetPassword.isPending}
              onClick={handleReset}
            >
              {t('admin.users.resetPasswordConfirmCta', { defaultValue: 'Sí, reiniciar' })}
            </Button>
          </div>
        </div>
      </Modal>

      {/* Generated password modal. Shown exactly once after either
          create-with-auto-password or reset-password. Closing this
          modal drops the plaintext from React state — there's no
          way to retrieve it again. The "Copy" affordance + the
          "share with the user" hint nudges the admin to act before
          they close. */}
      <Modal
        isOpen={generatedPasswordModal !== null}
        onClose={() => setGeneratedPasswordModal(null)}
        title={
          generatedPasswordModal?.kind === 'created'
            ? t('admin.users.generatedPasswordCreatedTitle', {
                defaultValue: 'Usuario creado',
              })
            : t('admin.users.generatedPasswordResetTitle', {
                defaultValue: 'Contraseña reiniciada',
              })
        }
        size="sm"
      >
        {generatedPasswordModal && (
          <div className="flex flex-col gap-4">
            <p className="text-sm text-text-secondary">
              <Trans
                i18nKey="admin.users.generatedPasswordHint"
                values={{ username: generatedPasswordModal.username }}
                defaults="Comparte esta contraseña con <strong>{{username}}</strong>. La verás solo una vez. Cuando inicie sesión se le pedirá que la cambie."
                components={{ strong: <strong className="text-text-primary" /> }}
              />
            </p>
            <div className="flex items-center gap-2 rounded-[--radius-md] border border-border bg-bg-elevated px-3 py-2 font-mono text-sm text-text-primary">
              <span className="flex-1 select-all break-all">
                {generatedPasswordModal.password}
              </span>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => {
                  void navigator.clipboard.writeText(
                    generatedPasswordModal.password,
                  );
                }}
              >
                {t('common.copy', { defaultValue: 'Copiar' })}
              </Button>
            </div>
            <div className="flex justify-end">
              <Button onClick={() => setGeneratedPasswordModal(null)}>
                {t('common.close', { defaultValue: 'Cerrar' })}
              </Button>
            </div>
          </div>
        )}
      </Modal>

      {/* Add profile modal — admin types a display name; the
          server synthesises username + a throwaway internal
          password. Profiles can't log in directly anyway. */}
      <Modal
        isOpen={profileParent !== null}
        onClose={() => {
          setProfileParent(null);
          setProfileName("");
        }}
        title={t('admin.users.addProfileTitle', { defaultValue: 'Añadir perfil' })}
        size="sm"
      >
        {profileParent && (
          <form onSubmit={handleCreateProfile} className="flex flex-col gap-4">
            <p className="text-sm text-text-secondary">
              <Trans
                i18nKey="admin.users.addProfileHelper"
                values={{ name: profileParent.username }}
                defaults="Esta persona compartirá el inicio de sesión de <strong>{{name}}</strong>. Cada miembro tiene sus propios favoritos, su historial y, si quieres, su PIN o un límite de edad."
                components={{ strong: <strong className="text-text-primary" /> }}
              />
            </p>
            <Input
              label={t('admin.users.profileName', { defaultValue: 'Nombre del miembro' })}
              placeholder={t('admin.users.profileNamePlaceholder', { defaultValue: 'Pedro' })}
              value={profileName}
              onChange={(e) => setProfileName(e.target.value)}
              autoFocus
              required
            />
            {createProfile.error && (
              <p className="text-xs text-error">{createProfile.error.message}</p>
            )}
            <div className="flex justify-end gap-3">
              <Button
                variant="secondary"
                type="button"
                onClick={() => {
                  setProfileParent(null);
                  setProfileName("");
                }}
              >
                {t('common.cancel')}
              </Button>
              <Button type="submit" isLoading={createProfile.isPending}>
                {t('common.create')}
              </Button>
            </div>
          </form>
        )}
      </Modal>

      {/* Rename modal — change display_name (visible label).
          Username + parent linkage stay put so logins / avatar
          colours / profile membership are unaffected. */}
      <Modal
        isOpen={renameTarget !== null}
        onClose={() => {
          setRenameTarget(null);
          setRenameValue("");
          setRenameColor("");
          setRenameError(null);
        }}
        title={t("admin.users.renameModalTitle", {
          defaultValue: "Personalizar perfil",
        })}
        size="sm"
      >
        {renameTarget && (
          <form onSubmit={handleRename} className="flex flex-col gap-4">
            <p className="text-sm text-text-secondary">
              {t("admin.users.renameModalHint", {
                defaultValue:
                  "Cambia el nombre y el color del avatar. El usuario interno (login) no cambia, así que las contraseñas y permisos siguen igual.",
              })}
            </p>
            <Input
              label={t("admin.users.displayName")}
              value={renameValue}
              onChange={(e) => setRenameValue(e.target.value)}
              autoFocus
              required
              maxLength={64}
            />

            {/* Colour picker — 14 swatches matching the avatar
                palette + an "auto" tile that clears the override
                so the deterministic FNV helper picks for you. */}
            <div className="flex flex-col gap-2">
              <span className="text-sm font-medium text-text-secondary">
                {t("admin.users.avatarColorLabel", {
                  defaultValue: "Color del avatar",
                })}
              </span>
              <div className="grid grid-cols-7 gap-2">
                <button
                  type="button"
                  onClick={() => setRenameColor("")}
                  className={[
                    "h-9 w-9 rounded-full border-2 text-[10px] font-medium transition-all",
                    renameColor === ""
                      ? "border-accent ring-2 ring-accent/30 text-text-primary"
                      : "border-border-subtle text-text-muted hover:border-border",
                  ].join(" ")}
                  title={t("admin.users.avatarColorAutoHint", {
                    defaultValue:
                      "Color automático según el nombre de usuario.",
                  })}
                  aria-label={t("admin.users.avatarColorAuto", {
                    defaultValue: "Auto",
                  })}
                >
                  A
                </button>
                {AVATAR_PALETTE.map((p) => {
                  const selected =
                    renameColor.toLowerCase() === p.background.toLowerCase();
                  return (
                    <button
                      type="button"
                      key={p.background}
                      onClick={() => setRenameColor(p.background)}
                      className={[
                        "h-9 w-9 rounded-full border-2 transition-all",
                        selected
                          ? "border-white scale-110 ring-2 ring-white/30"
                          : "border-transparent hover:scale-105",
                      ].join(" ")}
                      style={{ background: p.background }}
                      title={p.label}
                      aria-label={p.label}
                      aria-pressed={selected}
                    />
                  );
                })}
              </div>
            </div>

            {renameError && (
              <p className="text-xs text-error">{renameError}</p>
            )}
            <div className="flex justify-end gap-3">
              <Button
                type="button"
                variant="secondary"
                onClick={() => {
                  setRenameTarget(null);
                  setRenameValue("");
                  setRenameColor("");
                  setRenameError(null);
                }}
              >
                {t("common.cancel")}
              </Button>
              <Button
                type="submit"
                isLoading={
                  setUserDisplayName.isPending || setUserAvatarColor.isPending
                }
              >
                {t("common.save", { defaultValue: "Guardar" })}
              </Button>
            </div>
          </form>
        )}
      </Modal>

      {/* PIN modal — type 4 digits or leave empty to clear. */}
      <Modal
        isOpen={pinTarget !== null}
        onClose={() => {
          setPinTarget(null);
          setPinValue("");
          setPinError(null);
        }}
        title={t('admin.users.pinModalTitle', { defaultValue: 'PIN del perfil' })}
        size="sm"
      >
        {pinTarget && (
          <form onSubmit={handleSavePin} className="flex flex-col gap-4">
            <p className="text-sm text-text-secondary">
              {t('admin.users.pinModalHint', {
                defaultValue:
                  'Escribe un PIN de 4 dígitos o déjalo vacío para quitarlo. El PIN se pide al elegir el perfil en la pantalla de selección.',
              })}
            </p>
            <input
              type="tel"
              inputMode="numeric"
              pattern="[0-9]*"
              maxLength={4}
              autoFocus
              value={pinValue}
              onChange={(e) => setPinValue(e.target.value.replace(/[^0-9]/g, '').slice(0, 4))}
              placeholder="••••"
              className="w-32 self-center rounded-lg border border-border bg-bg-card px-4 py-2.5 text-center text-2xl font-mono tracking-[0.6em] text-text-primary focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/30"
            />
            {pinError && (
              <p className="text-xs text-error text-center">{pinError}</p>
            )}
            <div className="flex justify-end gap-3">
              <Button
                variant="secondary"
                type="button"
                onClick={() => {
                  setPinTarget(null);
                  setPinValue("");
                  setPinError(null);
                }}
              >
                {t('common.cancel')}
              </Button>
              <Button type="submit" isLoading={setUserPIN.isPending}>
                {pinValue === ''
                  ? t('admin.users.pinClearCta', { defaultValue: 'Quitar PIN' })
                  : t('common.save', { defaultValue: 'Guardar' })}
              </Button>
            </div>
          </form>
        )}
      </Modal>

      {/* Federation lived as its own top-level admin tab. It's a
          niche feature most installs never touch, but it IS about
          who's allowed to access this server's catalogue — so it
          fits naturally as a "Servidores conectados" section under
          the Users page. The dedicated /admin/federation route still
          works for direct links. */}
      <section className="mt-8 pt-6 border-t border-border-subtle">
        <FederationAdmin />
      </section>
    </div>
  );
}
