// Studio page entry — Phase 4 §10.
// Server-side: just resolves the image ID from the URL. The client child
// fetches the image record + annotation set via react-query so cache and
// optimistic updates live in one place.
import { StudioPage } from "./studio-page";

export default async function Studio({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <StudioPage imageId={id} />;
}
