import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/common";

export default function NotFound() {
  const { t } = useTranslation();
  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-4 px-4 text-center">
      <h1 className="text-7xl font-bold text-text-muted">{t('notFound.title')}</h1>
      <p className="text-xl text-text-secondary">{t('notFound.subtitle')}</p>
      <p className="max-w-sm text-sm text-text-muted">
        {t('notFound.description')}
      </p>
      <Link to="/" className="mt-4">
        <Button size="lg">{t('common.goHome')}</Button>
      </Link>
    </div>
  );
}
