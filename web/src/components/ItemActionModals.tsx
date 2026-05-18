// ItemActionModals — host global de los modales de acciones sobre
// items (Identify, Edit metadata). Vive una sola vez en App root y
// escucha al store useItemActions; cualquier kebab de poster en
// cualquier surface (home, /movies, /series, búsqueda) llama
// openIdentify(id) / openEditor(id) y el modal se materializa aquí.
//
// Esto evita que cada componente de card (PosterCard, LandscapeCard…)
// tenga que importar y hospedar los modales individualmente — sería
// 200 modales montados en una página con 200 cards.

import { useItem } from "@/api/hooks";
import { useItemActions } from "@/store/itemActions";
import { IdentifyDialog } from "./IdentifyDialog";
import { MetadataEditorDialog } from "./MetadataEditorDialog";
import { ImageManager } from "./ImageManager";

export function ItemActionModals() {
  const action = useItemActions((s) => s.action);
  const itemID = useItemActions((s) => s.itemID);
  const close = useItemActions((s) => s.close);

  // useItem está gated por el id. Cuando action=null el id es null
  // y la query no se dispara — coste cero hasta que el operador abre
  // un kebab. Caché compartido con la página detail si el operador
  // ya visitó el item antes.
  const itemQ = useItem(itemID ?? "", { enabled: !!itemID });

  if (!itemID || !action) return null;

  // ImageManager funciona sobre cualquier tipo (también seasons y
  // episodes, que tienen su propio poster/backdrop editable). Por eso
  // no esperamos a tener el itemQ resuelto — basta con el id.
  if (action === "images") {
    return <ImageManager itemId={itemID} isOpen={true} onClose={close} />;
  }

  // Identify / Edit metadata sí necesitan el item resuelto porque el
  // modal pre-rellena los inputs con title/year/overview actuales.
  if (!itemQ.data) return null;
  const item = itemQ.data;

  // Sólo películas y series para identify/editor — episodios y
  // temporadas heredan metadata del padre.
  if (item.type !== "movie" && item.type !== "series") return null;

  if (action === "identify") {
    return <IdentifyDialog isOpen={true} onClose={close} item={item} />;
  }
  return <MetadataEditorDialog isOpen={true} onClose={close} item={item} />;
}
