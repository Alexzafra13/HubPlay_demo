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

export function ItemActionModals() {
  const action = useItemActions((s) => s.action);
  const itemID = useItemActions((s) => s.itemID);
  const close = useItemActions((s) => s.close);

  // useItem está gated por el id. Cuando action=null el id es null
  // y la query no se dispara — coste cero hasta que el operador abre
  // un kebab. Caché compartido con la página detail si el operador
  // ya visitó el item antes.
  const itemQ = useItem(itemID ?? "", { enabled: !!itemID });

  if (!itemID || !action || !itemQ.data) return null;
  const item = itemQ.data;

  // Episodios y temporadas no aplican (heredan metadata del padre).
  // Si el kebab los emite por error, no abrimos nada.
  if (item.type !== "movie" && item.type !== "series") return null;

  if (action === "identify") {
    return <IdentifyDialog isOpen={true} onClose={close} item={item} />;
  }
  return <MetadataEditorDialog isOpen={true} onClose={close} item={item} />;
}
