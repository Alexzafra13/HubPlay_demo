import { useState } from "react";
import type { FormEvent } from "react";
import type { User } from "@/api/types";
import { useUsers, useCreateUser, useDeleteUser, useMe } from "@/api/hooks";
import { Button, Badge, Modal, Input, EmptyState, Skeleton } from "@/components/common";
import { useTranslation } from 'react-i18next';

export default function UsersAdmin() {
  const { t } = useTranslation();
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

  if (error) {
    return (
      <EmptyState
        title={t('admin.users.failedToLoad')}
        description={t('common.loadErrorHint')}
      />
    );
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
        <div className="overflow-x-auto rounded-[--radius-lg] border border-border">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-bg-elevated text-left text-text-muted">
                <th className="px-4 py-3 font-medium">{t('admin.users.username')}</th>
                <th className="px-4 py-3 font-medium">{t('admin.users.displayName')}</th>
                <th className="px-4 py-3 font-medium">{t('admin.users.role')}</th>
                <th className="px-4 py-3 font-medium">{t('admin.users.created')}</th>
                <th className="px-4 py-3 font-medium text-right">{t('admin.users.actions')}</th>
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
                          {t('admin.users.you')}
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
                          {t('common.delete')}
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

          <Input
            label={t('admin.users.password')}
            type="password"
            placeholder={t('admin.users.passwordPlaceholder')}
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            required
          />

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

      {/* Delete Confirmation Modal */}
      <Modal
        isOpen={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title={t('admin.users.deleteUser')}
        size="sm"
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-text-secondary">
            {t('admin.users.deleteConfirm', { name: deleteTarget?.username })}
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
    </div>
  );
}
