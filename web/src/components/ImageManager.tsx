import { useState, useRef, useCallback } from "react";
import type { FC } from "react";
import { useEffect } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import {
  useItemImages,
  useAvailableImages,
  useSelectImage,
  useUploadImage,
  useSetImagePrimary,
  useDeleteImage,
} from "@/api/hooks";
import type { ImageInfo, AvailableImage } from "@/api/types";
import { Spinner } from "@/components/common";
import { Button } from "@/components/common/Button";

interface ImageManagerProps {
  itemId: string;
  isOpen: boolean;
  onClose: () => void;
}

const IMAGE_TYPES = [
  { key: "primary", label: "imageManager.poster" },
  { key: "backdrop", label: "imageManager.backdrop" },
  { key: "logo", label: "imageManager.logo" },
  { key: "banner", label: "imageManager.banner" },
  { key: "thumb", label: "imageManager.thumb" },
] as const;

function getAspectRatioClass(imageType: string): string {
  return imageType === "primary" ? "aspect-[2/3]" : "aspect-video";
}

const ImageManager: FC<ImageManagerProps> = ({ itemId, isOpen, onClose }) => {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = useState<string>("primary");
  const [deleteConfirmId, setDeleteConfirmId] = useState<string | null>(null);
  const [statusMessage, setStatusMessage] = useState<{ type: "success" | "error"; text: string } | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const { data: images, isLoading: imagesLoading } = useItemImages(itemId);
  const { data: availableImages, isLoading: availableLoading } = useAvailableImages(itemId, activeTab);

  const selectImage = useSelectImage();
  const uploadImage = useUploadImage();
  const setImagePrimary = useSetImagePrimary();
  const deleteImage = useDeleteImage();

  const filteredImages = (images ?? []).filter((img) => img.type === activeTab);
  const filteredAvailable = (availableImages ?? []).filter((img) => img.type === activeTab);

  // Clear status message after a delay
  useEffect(() => {
    if (!statusMessage) return;
    const timer = setTimeout(() => setStatusMessage(null), 3000);
    return () => clearTimeout(timer);
  }, [statusMessage]);

  // Handle escape key
  const handleEscape = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    },
    [onClose],
  );

  useEffect(() => {
    if (!isOpen) return;
    document.addEventListener("keydown", handleEscape);
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", handleEscape);
      document.body.style.overflow = "";
    };
  }, [isOpen, handleEscape]);

  const handleSelectImage = useCallback(
    (img: AvailableImage) => {
      selectImage.mutate(
        { itemId, type: activeTab, url: img.url, width: img.width, height: img.height },
        {
          onSuccess: () => setStatusMessage({ type: "success", text: t("imageManager.success") }),
          onError: () => setStatusMessage({ type: "error", text: t("imageManager.error") }),
        },
      );
    },
    [itemId, activeTab, selectImage, t],
  );

  const handleUpload = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const file = e.target.files?.[0];
      if (!file) return;
      uploadImage.mutate(
        { itemId, type: activeTab, file },
        {
          onSuccess: () => setStatusMessage({ type: "success", text: t("imageManager.success") }),
          onError: () => setStatusMessage({ type: "error", text: t("imageManager.error") }),
        },
      );
      // Reset file input
      if (fileInputRef.current) fileInputRef.current.value = "";
    },
    [itemId, activeTab, uploadImage, t],
  );

  const handleSetPrimary = useCallback(
    (imageId: string) => {
      setImagePrimary.mutate(
        { itemId, imageId },
        {
          onSuccess: () => setStatusMessage({ type: "success", text: t("imageManager.success") }),
          onError: () => setStatusMessage({ type: "error", text: t("imageManager.error") }),
        },
      );
    },
    [itemId, setImagePrimary, t],
  );

  const handleDelete = useCallback(
    (imageId: string) => {
      deleteImage.mutate(
        { itemId, imageId },
        {
          onSuccess: () => {
            setStatusMessage({ type: "success", text: t("imageManager.success") });
            setDeleteConfirmId(null);
          },
          onError: () => setStatusMessage({ type: "error", text: t("imageManager.error") }),
        },
      );
    },
    [itemId, deleteImage, t],
  );

  if (!isOpen) return null;

  const isMutating = selectImage.isPending || uploadImage.isPending || setImagePrimary.isPending || deleteImage.isPending;

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/60 backdrop-blur-sm animate-fade-in"
        onClick={onClose}
        aria-hidden="true"
      />

      {/* Dialog */}
      <div
        role="dialog"
        aria-modal="true"
        aria-label={t("imageManager.title")}
        className="relative w-full max-w-4xl max-h-[85vh] flex flex-col rounded-[--radius-lg] bg-bg-card border border-border shadow-2xl animate-fade-in"
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-border shrink-0">
          <h2 className="text-lg font-semibold text-text-primary">{t("imageManager.title")}</h2>
          <button
            onClick={onClose}
            className="p-1 rounded-[--radius-sm] text-text-muted hover:text-text-primary hover:bg-bg-elevated transition-colors cursor-pointer"
            aria-label={t("common.close")}
          >
            <svg className="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
              <path d="M6.28 5.22a.75.75 0 00-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 101.06 1.06L10 11.06l3.72 3.72a.75.75 0 101.06-1.06L11.06 10l3.72-3.72a.75.75 0 00-1.06-1.06L10 8.94 6.28 5.22z" />
            </svg>
          </button>
        </div>

        {/* Status message */}
        {statusMessage && (
          <div
            className={[
              "mx-6 mt-4 rounded-[--radius-md] px-4 py-2 text-sm",
              statusMessage.type === "success"
                ? "bg-success/10 text-success"
                : "bg-error/10 text-error",
            ].join(" ")}
          >
            {statusMessage.text}
          </div>
        )}

        {/* Tabs */}
        <div className="flex gap-2 px-6 pt-4 pb-2 shrink-0 overflow-x-auto">
          {IMAGE_TYPES.map((type) => (
            <button
              key={type.key}
              type="button"
              onClick={() => setActiveTab(type.key)}
              className={[
                "shrink-0 rounded-[--radius-md] px-4 py-2 text-sm font-medium transition-colors cursor-pointer",
                activeTab === type.key
                  ? "bg-accent text-white"
                  : "bg-bg-elevated text-text-secondary hover:text-text-primary hover:bg-bg-card",
              ].join(" ")}
            >
              {t(type.label)}
            </button>
          ))}
        </div>

        {/* Scrollable body */}
        <div className="flex-1 overflow-y-auto px-6 py-4 space-y-6">
          {/* Current Images */}
          <section>
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-sm font-semibold text-text-primary uppercase tracking-wide">
                {t("imageManager.currentImages")}
              </h3>
              <div>
                <input
                  ref={fileInputRef}
                  type="file"
                  accept="image/jpeg,image/png,image/webp"
                  className="hidden"
                  onChange={handleUpload}
                />
                <Button
                  size="sm"
                  variant="secondary"
                  isLoading={uploadImage.isPending}
                  onClick={() => fileInputRef.current?.click()}
                >
                  <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                    <path strokeLinecap="round" strokeLinejoin="round" d="M12 4v16m8-8H4" />
                  </svg>
                  {uploadImage.isPending ? t("imageManager.uploading") : t("imageManager.upload")}
                </Button>
              </div>
            </div>

            {imagesLoading ? (
              <div className="flex justify-center py-8">
                <Spinner size="md" />
              </div>
            ) : filteredImages.length === 0 ? (
              <p className="text-sm text-text-muted py-4">{t("imageManager.noCurrentImages")}</p>
            ) : (
              <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-3">
                {filteredImages.map((img) => (
                  <CurrentImageCard
                    key={img.id}
                    image={img}
                    isDeleteConfirm={deleteConfirmId === img.id}
                    isMutating={isMutating}
                    aspectRatioClass={getAspectRatioClass(activeTab)}
                    onSetPrimary={() => handleSetPrimary(img.id)}
                    onDeleteClick={() => setDeleteConfirmId(img.id)}
                    onDeleteConfirm={() => handleDelete(img.id)}
                    onDeleteCancel={() => setDeleteConfirmId(null)}
                    t={t}
                  />
                ))}
              </div>
            )}
            <p className="text-xs text-text-muted mt-2">{t("imageManager.uploadHint")}</p>
          </section>

          {/* Available Images */}
          <section>
            <h3 className="text-sm font-semibold text-text-primary uppercase tracking-wide mb-3">
              {t("imageManager.availableImages")}
            </h3>

            {availableLoading ? (
              <div className="flex justify-center py-8">
                <Spinner size="md" />
              </div>
            ) : filteredAvailable.length === 0 ? (
              <p className="text-sm text-text-muted py-4">{t("imageManager.noAvailableImages")}</p>
            ) : (
              <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-3">
                {filteredAvailable.map((img, index) => (
                  <AvailableImageCard
                    key={`${img.url}-${index}`}
                    image={img}
                    isSelecting={selectImage.isPending}
                    aspectRatioClass={getAspectRatioClass(activeTab)}
                    onSelect={() => handleSelectImage(img)}
                    t={t}
                  />
                ))}
              </div>
            )}
          </section>
        </div>
      </div>
    </div>,
    document.body,
  );
};

// ─── Sub-components ──────────────────────────────────────────────────────────

interface CurrentImageCardProps {
  image: ImageInfo;
  isDeleteConfirm: boolean;
  isMutating: boolean;
  aspectRatioClass: string;
  onSetPrimary: () => void;
  onDeleteClick: () => void;
  onDeleteConfirm: () => void;
  onDeleteCancel: () => void;
  t: (key: string, opts?: Record<string, unknown>) => string;
}

const CurrentImageCard: FC<CurrentImageCardProps> = ({
  image,
  isDeleteConfirm,
  isMutating,
  aspectRatioClass,
  onSetPrimary,
  onDeleteClick,
  onDeleteConfirm,
  onDeleteCancel,
  t,
}) => (
  <div className="group relative rounded-[--radius-md] border border-border bg-bg-elevated overflow-hidden">
    <img
      src={image.path}
      alt={image.type}
      className={`w-full ${aspectRatioClass} object-cover`}
      loading="lazy"
    />

    {/* Primary badge */}
    {image.is_primary && (
      <div className="absolute top-2 left-2 flex items-center gap-1 rounded-full bg-accent/90 px-2 py-0.5 text-xs font-medium text-white">
        <svg className="h-3 w-3" viewBox="0 0 24 24" fill="currentColor">
          <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" />
        </svg>
        {t("imageManager.primary")}
      </div>
    )}

    {/* Resolution overlay */}
    {image.width && image.height && (
      <div className="absolute bottom-0 left-0 right-0 bg-black/60 px-2 py-1 text-xs text-white text-center">
        {t("imageManager.resolution", { width: image.width, height: image.height })}
      </div>
    )}

    {/* Hover overlay with actions */}
    <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 bg-black/70 opacity-0 group-hover:opacity-100 transition-opacity">
      {isDeleteConfirm ? (
        <div className="flex flex-col items-center gap-2 px-2">
          <p className="text-xs text-white text-center">{t("imageManager.deleteConfirm")}</p>
          <div className="flex gap-2">
            <Button size="sm" variant="danger" disabled={isMutating} onClick={onDeleteConfirm}>
              {t("imageManager.delete")}
            </Button>
            <Button size="sm" variant="secondary" onClick={onDeleteCancel}>
              {t("common.cancel")}
            </Button>
          </div>
        </div>
      ) : (
        <>
          {!image.is_primary && (
            <Button size="sm" variant="primary" disabled={isMutating} onClick={onSetPrimary}>
              {t("imageManager.setPrimary")}
            </Button>
          )}
          <Button size="sm" variant="danger" disabled={isMutating} onClick={onDeleteClick}>
            {t("imageManager.delete")}
          </Button>
        </>
      )}
    </div>
  </div>
);

interface AvailableImageCardProps {
  image: AvailableImage;
  isSelecting: boolean;
  aspectRatioClass: string;
  onSelect: () => void;
  t: (key: string, opts?: Record<string, unknown>) => string;
}

const AvailableImageCard: FC<AvailableImageCardProps> = ({
  image,
  isSelecting,
  aspectRatioClass,
  onSelect,
  t,
}) => (
  <div className="group relative rounded-[--radius-md] border border-border bg-bg-elevated overflow-hidden cursor-pointer" onClick={onSelect}>
    <img
      src={image.url}
      alt={image.type}
      className={`w-full ${aspectRatioClass} object-cover`}
      loading="lazy"
    />

    {/* Info overlay */}
    <div className="absolute bottom-0 left-0 right-0 bg-black/70 px-2 py-1.5 text-xs text-white space-y-0.5">
      <div>{t("imageManager.resolution", { width: image.width, height: image.height })}</div>
      {image.score > 0 && (
        <div className="text-text-muted">{t("imageManager.score", { score: image.score.toFixed(1) })}</div>
      )}
      {image.language && (
        <div className="uppercase text-text-muted">{image.language}</div>
      )}
    </div>

    {/* Hover overlay */}
    <div className="absolute inset-0 flex items-center justify-center bg-black/60 opacity-0 group-hover:opacity-100 transition-opacity">
      <Button size="sm" variant="primary" isLoading={isSelecting} disabled={isSelecting}>
        {isSelecting ? t("imageManager.selecting") : t("imageManager.selectImage")}
      </Button>
    </div>
  </div>
);

export { ImageManager };
export type { ImageManagerProps };
