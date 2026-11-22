import type { NavItem } from "../../layout/nav";
import { PageHeader } from "../../components/PageHeader";
import { Panel } from "../../components/Panel";
import { EmptyState } from "../../components/States";

export default function ComingSoon({ item }: { item: NavItem }) {
  return (
    <>
      <PageHeader title={item.label} />
      <Panel>
        <EmptyState code="…" title="On progress implementasi" />
      </Panel>
    </>
  );
}