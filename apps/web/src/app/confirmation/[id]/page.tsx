import ConfirmationPage from "./confirmation-page";

export async function generateStaticParams() {
  return [{ id: "fallback" }];
}

export default function Page() {
  return <ConfirmationPage />;
}
