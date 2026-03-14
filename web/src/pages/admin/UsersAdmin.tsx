import { useState } from "react";
import type { FormEvent } from "react";
import type { User } from "@/api/types";
import { useUsers, useCreateUser, useDeleteUser, useMe } from "@/api/hooks";
import { Button, Badge, Modal, Input, Spinner, EmptyState } from "@/components/common";

export default function UsersAdmin() {
  const { data: me } = useMe();
  const { data: users, isLoading, error } = useUsers();
  const createUser = useCreateUser();
  const deleteUser = useDeleteUser();

  const [showAddModal, setShowAddModal] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<User | null>(null);

  // Add user form state
  const [newUsername, setNewUsername] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [newDisplayName, setNewDisplayName] = useState("");
  const [newRole, setNewRole] = useState("user");

  function resetForm() {
    setNewUsername("");
    setNewPassword("");
    setNewDisplayName("");
    setNewRole("user");
  }

  function handleCreate(e: FormEvent) {
    e.preventDefault();
    if (!newUsername.trim() || !newPassword.trim()) return;

    createUser.mutate(
      {
        username: newUsername.trim(),
        password: newPassword,
        display_name: newDisplayName.trim() || undefined,
        role: newRole,
      },
      {
        onSuccess: () => {
          setShowAddModal(false);
          resetForm();
        },
      },
    );
  }

  function handleDelete() {
    if (!deleteTarget) return;
    deleteUser.mutate(deleteTarget.id, {
      onSuccess: () => setDeleteTarget(null),
    });
  }

  function formatDate(iso: string) {
    return new Date(iso).toLocaleDateString(undefined, {
      year: "numeric",
      month: "short",
      day: "numeric",
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
      <EmptyState title="Failed to load users" description={error.message} />
    );
  }

  return (
    <div className="flex flex-col gap-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-text-primary">Users</h2>
        <Button onClick={() => setShowAddModal(true)}>Add User</Button>
      </div>

      {/* Table */}
      {users && users.length > 0 ? (
        <div className="overflow-x-auto rounded-[--radius-lg] border border-border">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-bg-elevated text-left text-text-muted">
                <th className="px-4 py-3 font-medium">Username</th>
                <th className="px-4 py-3 font-medium">Display Name</th>
                <th className="px-4 py-3 font-medium">Role</th>
                <th className="px-4 py-3 font-medium">Created</th>
                <th className="px-4 py-3 font-medium text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {users.map((user) => {
                const isSelf = me?.id === user.id;
                return (
                  <tr
                    key={user.id}
                    className="bg-bg-card hover:bg-bg-elevated transition-colors"
                  >
                    <td className="px-4 py-3 font-medium text-text-primary">
                      {user.username}
                      {isSelf && (
                        <span className="ml-2 text-xs text-text-muted">
                          (you)
                        </span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-text-secondary">
                      {user.display_name}
                    </td>
                    <td className="px-4 py-3">
                      <Badge
                        variant={user.role === "admin" ? "warning" : "default"}
                      >
                        {user.role}
                      </Badge>
                    </td>
                    <td className="px-4 py-3 text-text-secondary">
                      {formatDate(user.created_at)}
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex justify-end">
                        <Button
                          variant="danger"
                          size="sm"
                          disabled={isSelf}
                          onClick={() => setDeleteTarget(user)}
                        >
                          Delete
                        </Button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      ) : (
        <EmptyState
          title="No users"
          description="No user accounts found."
        />
      )}

      {/* Add User Modal */}
      <Modal
        isOpen={showAddModal}
        onClose={() => setShowAddModal(false)}
        title="Add User"
      >
        <form onSubmit={handleCreate} className="flex flex-col gap-4">
          <Input
            label="Username"
            placeholder="johndoe"
            value={newUsername}
            onChange={(e) => setNewUsername(e.target.value)}
            required
          />

          <Input
            label="Password"
            type="password"
            placeholder="Enter password"
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            required
          />

          <Input
            label="Display Name"
            placeholder="John Doe"
            value={newDisplayName}
            onChange={(e) => setNewDisplayName(e.target.value)}
          />

          <div className="flex flex-col gap-1.5">
            <label
              htmlFor="user-role"
              className="text-sm font-medium text-text-secondary"
            >
              Role
            </label>
            <select
              id="user-role"
              value={newRole}
              onChange={(e) => setNewRole(e.target.value)}
              className="w-full rounded-[--radius-md] bg-bg-card border border-border px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/30"
            >
              <option value="user">User</option>
              <option value="admin">Admin</option>
            </select>
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
              Cancel
            </Button>
            <Button type="submit" isLoading={createUser.isPending}>
              Create
            </Button>
          </div>
        </form>
      </Modal>

      {/* Delete Confirmation Modal */}
      <Modal
        isOpen={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title="Delete User"
        size="sm"
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-text-secondary">
            Are you sure you want to delete user{" "}
            <span className="font-semibold text-text-primary">
              {deleteTarget?.username}
            </span>
            ? This action cannot be undone.
          </p>

          {deleteUser.error && (
            <p className="text-xs text-error">{deleteUser.error.message}</p>
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
              isLoading={deleteUser.isPending}
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
