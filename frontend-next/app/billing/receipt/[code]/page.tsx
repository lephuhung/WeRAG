import { AuthProvider } from "@/lib/auth";
import { RequireAuth } from "@/components/require-auth";
import { OrderReceiptPage } from "@/components/billing/order-receipt";

/* Printable payment receipt (biên nhận thanh toán) of a paid order, outside
 * the console frame so it prints on a clean A4 page. Access is the order's
 * (buyer, Tổ chức admins, SuperAdmin), checked by the API. */
export default async function ReceiptPage({ params }: { params: Promise<{ code: string }> }) {
  const { code } = await params;
  return (
    <AuthProvider>
      <RequireAuth>
        <OrderReceiptPage code={code} />
      </RequireAuth>
    </AuthProvider>
  );
}
