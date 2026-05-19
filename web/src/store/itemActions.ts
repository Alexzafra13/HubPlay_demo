// useItemActions — store global para abrir el editor/identify de un
// item desde cualquier sitio (kebab en posters de home, grids, etc.)
// sin que cada surface tenga que hostear sus propios modales.
//
// Patrón: hay UN solo modal del editor y UN solo del identify montados
// en el árbol (vía <ItemActionModals/> en App root). Cuando un kebab
// llama openEditor(id) o openIdentify(id), el store guarda el id +
// el tipo, los modales se hidratan vía useItem(id) y se renderizan.
// Cierre = clear del store.
//
// Comparable al patrón de useModalStack pero específico para acciones
// sobre items — éste guarda la INTENCIÓN (qué item, qué acción), el
// modalStack se encarga del foco / escape / scroll lock una vez el
// modal real se monta.

import { create } from "zustand";

export type ItemAction = "identify" | "edit-metadata" | "images";

interface ItemActionsState {
  // Acción activa + id + tipo del item al que aplica. null cuando no
  // hay modal de acción abierto (estado por defecto).
  // itemType se guarda para que el ImageManager pueda filtrar las
  // pestañas (poster/backdrop/...) sin tener que esperar el round-
  // trip de useItem cada vez que se abre el modal desde un poster
  // del listado.
  action: ItemAction | null;
  itemID: string | null;
  itemType: string | null;
  openIdentify: (itemID: string, itemType: string) => void;
  openEditor: (itemID: string, itemType: string) => void;
  openImages: (itemID: string, itemType: string) => void;
  close: () => void;
}

export const useItemActions = create<ItemActionsState>((set) => ({
  action: null,
  itemID: null,
  itemType: null,
  openIdentify: (itemID, itemType) =>
    set({ action: "identify", itemID, itemType }),
  openEditor: (itemID, itemType) =>
    set({ action: "edit-metadata", itemID, itemType }),
  openImages: (itemID, itemType) =>
    set({ action: "images", itemID, itemType }),
  close: () => set({ action: null, itemID: null, itemType: null }),
}));
