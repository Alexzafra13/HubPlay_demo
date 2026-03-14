import { useState } from "react";
import type { FormEvent } from "react";
import type { ContentType, Library } from "@/api/types";
import {
  useLibraries,
  useCreateLibrary,
  useScanLibrary,
  useDeleteLibrary,
} from "@/api/hooks";
import { Button, Badge, Modal, Input, Spinner, EmptyState } from "@/components/common";

const CONTENT_TYPES: { value: ContentType; label: string }[] = [
  { value: "movies", label: "Movies" },
  { value: "tvshows", label: "TV Shows" },
  { value: "livetv", label: "Live TV" },
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
  const { data: libraries, isLoading, error } = useLibraries();
  const createLibrary = useCreateLibrary();
  const scanLibrary = useScanLibrary();
  const deleteLibrary = useDeleteLibrary();

  const [showAddModal, setShowAddModal] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Library | null>(null);

  // Add library form state
  const [newName, setNewName] = useState("");
  const [newType, setNewType] = useState<ContentType>("movies");
  const [newPath, setNewPath] = useState("");

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
        title="Failed to load libraries"
        description={error.message}
      />
    );
  }

  return (
    <div className="flex flex-col gap-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-text-primary">
          Media Libraries
        </h2>
        <Button onClick={() => setShowAddModal(true)}>Add Library</Button>
      </div>

      {/* Table */}
      {libraries && libraries.length > 0 ? (
        <div className="overflow-x-auto rounded-[--radius-lg] border border-border">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-bg-elevated text-left text-text-muted">
                <th className="px-4 py-3 font-medium">Name</th>
                <th className="px-4 py-3 font-medium">Type</th>
                <th className="px-4 py-3 font-medium">Path</th>
                <th className="px-4 py-3 font-medium text-right">Items</th>
                <th className="px-4 py-3 font-medium">Scan Status</th>
                <th className="px-4 py-3 font-medium text-right">Actions</th>
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
                    {lib.paths.join(", ")}
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
                          scanLibrary.variables === lib.id
                        }
                        disabled={lib.scan_status === "scanning"}
                        onClick={() => scanLibrary.mutate(lib.id)}
                      >
                        Scan
                      </Button>
                      <Button variant="ghost" size="sm" disabled>
                        Edit
                      </Button>
                      <Button
                        variant="danger"
                        size="sm"
                        onClick={() => setDeleteTarget(lib)}
                      >
                        Delete
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
          title="No libraries"
          description="Add a media library to get started."
          action={
            <Button onClick={() => setShowAddModal(true)}>
              Add Library
            </Button>
          }
        />
      )}

      {/* Add Library Modal */}
      <Modal
        isOpen={showAddModal}
        onClose={() => setShowAddModal(false)}
        title="Add Library"
      >
        <form onSubmit={handleCreate} className="flex flex-col gap-4">
          <Input
            label="Name"
            placeholder="e.g. Movies"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            required
          />

          <div className="flex flex-col gap-1.5">
            <label
              htmlFor="content-type"
              className="text-sm font-medium text-text-secondary"
            >
              Content Type
            </label>
            <select
              id="content-type"
              value={newType}
              onChange={(e) => setNewType(e.target.value as ContentType)}
              className="w-full rounded-[--radius-md] bg-bg-card border border-border px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/30"
            >
              {CONTENT_TYPES.map((ct) => (
                <option key={ct.value} value={ct.value}>
                  {ct.label}
                </option>
              ))}
            </select>
          </div>

          <Input
            label="Path"
            placeholder="/media/movies"
            value={newPath}
            onChange={(e) => setNewPath(e.target.value)}
            required
          />

          {createLibrary.error && (
            <p className="text-xs text-error">{createLibrary.error.message}</p>
          )}

          <div className="flex justify-end gap-3 pt-2">
            <Button
              variant="secondary"
              type="button"
              onClick={() => setShowAddModal(false)}
            >
              Cancel
            </Button>
            <Button type="submit" isLoading={createLibrary.isPending}>
              Create
            </Button>
          </div>
        </form>
      </Modal>

      {/* Delete Confirmation Modal */}
      <Modal
        isOpen={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title="Delete Library"
        size="sm"
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-text-secondary">
            Are you sure you want to delete{" "}
            <span className="font-semibold text-text-primary">
              {deleteTarget?.name}
            </span>
            ? This will remove the library and all associated metadata. Media
            files on disk will not be affected.
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
              Cancel
            </Button>
            <Button
              variant="danger"
              isLoading={deleteLibrary.isPending}
              onClick={handleDelete}
            >
              Delete
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
