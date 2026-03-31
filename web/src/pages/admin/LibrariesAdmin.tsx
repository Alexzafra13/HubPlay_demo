import { useState } from "react";
import type { FormEvent } from "react";
import type { ContentType, Library } from "@/api/types";
import {
  useLibraries,
  useCreateLibrary,
  useUpdateLibrary,
  useScanLibrary,
  useDeleteLibrary,
} from "@/api/hooks";
import { Button, Badge, Modal, Input, Spinner, EmptyState } from "@/components/common";
import { FolderBrowser } from "@/components/setup/FolderBrowser";
import { useTranslation } from 'react-i18next';

const CONTENT_TYPES: { value: ContentType; key: string }[] = [
  { value: "movies", key: "contentTypes.movies" },
  { value: "tvshows", key: "contentTypes.tvShows" },
  { value: "livetv", key: "contentTypes.liveTV" },
];

function scanStatusVariant(status: string) {
  switch (status) {
    case "scanning":
      return "warning" as const;
    case "error":
      return "error" as const;
    default:
      return "success" as const;
  }
}

function contentTypeBadge(type: string) {
  switch (type) {
    case "movies":
      return "default" as const;
    case "tvshows":
      return "default" as const;
    case "livetv":
      return "live" as const;
    default:
      return "default" as const;
  }
}

export default function LibrariesAdmin() {
  const { t } = useTranslation();
  const { data: libraries, isLoading, error } = useLibraries();
  const createLibrary = useCreateLibrary();
  const updateLibrary = useUpdateLibrary();
  const scanLibrary = useScanLibrary();
  const deleteLibrary = useDeleteLibrary();

  const [showAddModal, setShowAddModal] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Library | null>(null);
  const [editTarget, setEditTarget] = useState<Library | null>(null);

  // Add library form state
  const [newName, setNewName] = useState("");
  const [newType, setNewType] = useState<ContentType>("movies");
  const [newPath, setNewPath] = useState("");
  const [showCreateBrowse, setShowCreateBrowse] = useState(false);

  // Edit library form state
  const [editName, setEditName] = useState("");
  const [editPath, setEditPath] = useState("");
  const [showEditBrowse, setShowEditBrowse] = useState(false);

  function openEditModal(lib: Library) {
    setEditTarget(lib);
    setEditName(lib.name);
    setEditPath((lib.paths ?? [])[0] ?? "");
  }

  function handleCreate(e: FormEvent) {
    e.preventDefault();
    if (!newName.trim() || !newPath.trim()) return;

    createLibrary.mutate(
      { name: newName.trim(), content_type: newType, paths: [newPath.trim()] },
      {
        onSuccess: () => {
          setShowAddModal(false);
          setNewName("");
          setNewType("movies");
          setNewPath("");
        },
      },
    );
  }

  function handleEdit(e: FormEvent) {
    e.preventDefault();
    if (!editTarget || !editName.trim() || !editPath.trim()) return;

    updateLibrary.mutate(
      {
        id: editTarget.id,
        data: { name: editName.trim(), paths: [editPath.trim()] },
      },
      {
        onSuccess: () => setEditTarget(null),
      },
    );
  }

  function handleDelete() {
    if (!deleteTarget) return;
    deleteLibrary.mutate(deleteTarget.id, {
      onSuccess: () => setDeleteTarget(null),
    });
  }

  if (isLoading) {
    return (
      <div className="flex justify-center py-16">
        <Spinner size="lg" />
      </div>
    );
  }

  if (error) {
    return (
      <EmptyState
        title={t('admin.libraries.failedToLoad')}
        description={error.message}
      />
    );
  }

  return (
    <div className="flex flex-col gap-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-text-primary">
          {t('admin.libraries.title')}
        </h2>
        <Button onClick={() => setShowAddModal(true)}>{t('admin.libraries.addLibrary')}</Button>
      </div>

      {/* Table */}
      {libraries && libraries.length > 0 ? (
        <div className="overflow-x-auto rounded-[--radius-lg] border border-border">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-bg-elevated text-left text-text-muted">
                <th className="px-4 py-3 font-medium">{t('admin.libraries.name')}</th>
                <th className="px-4 py-3 font-medium">{t('admin.libraries.type')}</th>
                <th className="px-4 py-3 font-medium">{t('admin.libraries.path')}</th>
                <th className="px-4 py-3 font-medium text-right">{t('admin.libraries.itemCount')}</th>
                <th className="px-4 py-3 font-medium">{t('admin.libraries.scanStatus')}</th>
                <th className="px-4 py-3 font-medium text-right">{t('admin.libraries.actions')}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {libraries.map((lib) => (
                <tr
                  key={lib.id}
                  className="bg-bg-card hover:bg-bg-elevated transition-colors"
                >
                  <td className="px-4 py-3 font-medium text-text-primary">
                    {lib.name}
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant={contentTypeBadge(lib.content_type)}>
                      {lib.content_type}
                    </Badge>
                  </td>
                  <td className="px-4 py-3 text-text-secondary font-mono text-xs">
                    {(lib.paths ?? []).join(", ")}
                  </td>
                  <td className="px-4 py-3 text-right text-text-secondary tabular-nums">
                    {lib.item_count}
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant={scanStatusVariant(lib.scan_status)}>
                      {lib.scan_status}
                    </Badge>
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center justify-end gap-2">
                      <Button
                        variant="secondary"
                        size="sm"
                        isLoading={
                          scanLibrary.isPending &&
                          scanLibrary.variables?.id === lib.id &&
                          !scanLibrary.variables?.refreshMetadata
                        }
                        disabled={lib.scan_status === "scanning"}
                        onClick={() => scanLibrary.mutate({ id: lib.id })}
                      >
                        {t('admin.libraries.scan')}
                      </Button>
                      <Button
                        variant="secondary"
                        size="sm"
                        isLoading={
                          scanLibrary.isPending &&
                          scanLibrary.variables?.id === lib.id &&
                          !!scanLibrary.variables?.refreshMetadata
                        }
                        disabled={lib.scan_status === "scanning"}
                        onClick={() => scanLibrary.mutate({ id: lib.id, refreshMetadata: true })}
                        title={t('admin.libraries.refreshMetadataTooltip')}
                      >
                        {t('admin.libraries.refreshMetadata')}
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => openEditModal(lib)}
                      >
                        {t('common.edit')}
                      </Button>
                      <Button
                        variant="danger"
                        size="sm"
                        onClick={() => setDeleteTarget(lib)}
                      >
                        {t('common.delete')}
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <EmptyState
          title={t('admin.libraries.noLibraries')}
          description={t('admin.libraries.noLibrariesHint')}
          action={
            <Button onClick={() => setShowAddModal(true)}>
              {t('admin.libraries.addLibrary')}
            </Button>
          }
        />
      )}

      {/* Add Library Modal */}
      <Modal
        isOpen={showAddModal}
        onClose={() => setShowAddModal(false)}
        title={t('admin.libraries.addLibrary')}
      >
        <form onSubmit={handleCreate} className="flex flex-col gap-4">
          <Input
            label={t('admin.libraries.name')}
            placeholder={t('admin.libraries.namePlaceholder')}
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            required
          />

          <div className="flex flex-col gap-1.5">
            <label
              htmlFor="content-type"
              className="text-sm font-medium text-text-secondary"
            >
              {t('admin.libraries.contentType')}
            </label>
            <select
              id="content-type"
              value={newType}
              onChange={(e) => setNewType(e.target.value as ContentType)}
              className="w-full rounded-[--radius-md] bg-bg-card border border-border px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/30"
            >
              {CONTENT_TYPES.map((ct) => (
                <option key={ct.value} value={ct.value}>
                  {t(ct.key)}
                </option>
              ))}
            </select>
          </div>

          <div className="flex gap-2 items-end">
            <div className="flex-1">
              <Input
                label={t('admin.libraries.path')}
                placeholder={t('admin.libraries.pathPlaceholder')}
                value={newPath}
                onChange={(e) => setNewPath(e.target.value)}
                required
              />
            </div>
            <Button
              type="button"
              variant="secondary"
              onClick={() => setShowCreateBrowse(true)}
            >
              {t('common.browse')}
            </Button>
          </div>

          {createLibrary.error && (
            <p className="text-xs text-error">{createLibrary.error.message}</p>
          )}

          <div className="flex justify-end gap-3 pt-2">
            <Button
              variant="secondary"
              type="button"
              onClick={() => setShowAddModal(false)}
            >
              {t('common.cancel')}
            </Button>
            <Button type="submit" isLoading={createLibrary.isPending}>
              {t('common.create')}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Edit Library Modal */}
      <Modal
        isOpen={editTarget !== null}
        onClose={() => setEditTarget(null)}
        title={t('admin.libraries.editLibrary')}
      >
        <form onSubmit={handleEdit} className="flex flex-col gap-4">
          <Input
            label={t('admin.libraries.name')}
            placeholder={t('admin.libraries.namePlaceholder')}
            value={editName}
            onChange={(e) => setEditName(e.target.value)}
            required
          />

          <div className="flex gap-2 items-end">
            <div className="flex-1">
              <Input
                label={t('admin.libraries.path')}
                placeholder={t('admin.libraries.pathPlaceholder')}
                value={editPath}
                onChange={(e) => setEditPath(e.target.value)}
                required
              />
            </div>
            <Button
              type="button"
              variant="secondary"
              onClick={() => setShowEditBrowse(true)}
            >
              {t('common.browse')}
            </Button>
          </div>

          {updateLibrary.error && (
            <p className="text-xs text-error">{updateLibrary.error.message}</p>
          )}

          <div className="flex justify-end gap-3 pt-2">
            <Button
              variant="secondary"
              type="button"
              onClick={() => setEditTarget(null)}
            >
              {t('common.cancel')}
            </Button>
            <Button type="submit" isLoading={updateLibrary.isPending}>
              {t('common.save')}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Delete Confirmation Modal */}
      <Modal
        isOpen={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title={t('admin.libraries.deleteLibrary')}
        size="sm"
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-text-secondary">
            {t('admin.libraries.deleteConfirm', { name: deleteTarget?.name })}
          </p>

          {deleteLibrary.error && (
            <p className="text-xs text-error">
              {deleteLibrary.error.message}
            </p>
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
              isLoading={deleteLibrary.isPending}
              onClick={handleDelete}
            >
              {t('common.delete')}
            </Button>
          </div>
        </div>
      </Modal>

      {/* Folder Browsers */}
      <FolderBrowser
        isOpen={showCreateBrowse}
        onClose={() => setShowCreateBrowse(false)}
        onSelect={(path) => {
          setNewPath(path);
          if (!newName.trim()) {
            const segments = path.split("/").filter(Boolean);
            setNewName(segments[segments.length - 1] ?? "");
          }
        }}
        useAdmin
      />

      <FolderBrowser
        isOpen={showEditBrowse}
        onClose={() => setShowEditBrowse(false)}
        onSelect={(path) => setEditPath(path)}
        useAdmin
      />
    </div>
  );
}
