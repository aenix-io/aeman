import type { CustomerRef, Provider } from "../providers/types";
import { TeamsModal } from "./TeamsModal";

/** CustomersModal manages the board's customer roster (ownership.md, O5)
 *  with the dialog the teams use: add, rename, reorder,
 *  delete. Cards name a customer by id, so a rename reaches every card at
 *  once and a delete takes it off every card that names it. */
export function CustomersModal({
  customers,
  provider,
  onChanged,
  onError,
  onClose,
}: {
  customers: readonly CustomerRef[];
  provider: Pick<Provider, "addCustomer" | "renameCustomer" | "reorderCustomers" | "deleteCustomer">;
  /** The roster changed on the server; the board reads it back. */
  onChanged: () => void;
  onError: (message: string) => void;
  onClose: () => void;
}) {
  const idOf = (name: string) => customers.find((c) => c.name === name)?.id ?? "";
  const run = (p: Promise<unknown>) =>
    void p.then(onChanged).catch((err: unknown) => onError(err instanceof Error ? err.message : String(err)));
  return (
    <TeamsModal
      title="Customers"
      entity="customer"
      teams={customers.map((c) => c.name)}
      onAdd={(name) => run(provider.addCustomer(name))}
      onRename={(from, to) => run(provider.renameCustomer(idOf(from), to))}
      onRemove={(name) => {
        if (window.confirm(`Delete the customer «${name}»? It comes off every card that names it.`)) {
          run(provider.deleteCustomer(idOf(name)));
        }
      }}
      onReorder={(names) => run(provider.reorderCustomers(names.map(idOf).filter(Boolean)))}
      onClose={onClose}
    />
  );
}
