# Security policy

## Reporting a vulnerability

Report vulnerabilities privately through the contact details on the maintainer's GitHub profile, [github.com/reorx](https://github.com/reorx). Please do not post details in public issues or pull requests. Include the tinyhost version, deployment conditions, reproduction steps, expected and actual behavior, and the impact. Remove real credentials and private content from examples.

If you cannot find a way to reach the maintainer privately, open an issue that asks for a contact channel without disclosing the vulnerability.

## Security model

tinyhost is built for a single owner and approved agents. It serves static files, including JavaScript, and does not execute uploaded server-side programs. Approved agents share publishing access to Apps; approval is not an App-specific permission grant.

- **Two hostnames are required.** Published Apps, previews and the Agent API live on the apps hostname. The owner signs in and approves agents on a different admin hostname. Published content is never served at `/` or `/_/`, and the server does not emit `Service-Worker-Allowed` to broaden an App's worker scope.
- **Write credentials are separate from viewing credentials.** Agent endpoints accept `Authorization: Bearer` only. Admin operations use the host-only `__Host-th_admin` session cookie, with Origin validation on every POST (falling back to `Sec-Fetch-Site: same-origin` when Origin is absent). The apps hostname's `__Host-th_view` cookie grants read access to private content only. It never authorizes a write.
- **Passwords belong on the admin hostname.** Private viewing uses a short-lived, one-time ticket exchanged for an apps viewing session; no password form is served on the apps hostname. Owner passwords are stored with bcrypt.
- **Server-side credentials are hashed at rest.** The server stores Agent tokens, browser session IDs and one-time viewing tickets as SHA-256 hashes. Newly issued Agent tokens are only returned once. Preview tokens are an explicit exception: their plaintext is stored so their short-lived links can be displayed in the console. They never grant write access. Database files use mode `0600`. The CLI keeps its usable token in a local credential file with mode `0600`; protect that file as a secret.
- **Previews are link-accessible.** Anyone with a preview URL can read its content without signing in until expiry or deletion. Treat the URL as shared access, and avoid putting secrets in previews.
- **HTTPS is required for deployment.** Secure, host-only cookies are the default. The insecure-cookie option is only for local development. Keep the backend port private behind a reverse proxy and preserve the incoming Host header.

## Accepted shared-origin and same-site risks

All Apps on an instance share the apps origin. In a browser with a private viewing session, JavaScript in one App can fetch other private Apps and exfiltrate their content. Private visibility protects against unauthenticated visitors; it does not isolate one App from another. A malicious App can also imitate a login screen. Host only trusted agent output and enter the owner password only on the admin hostname.

Sibling hostnames under one registrable domain are same-site. A script on the apps hostname can initiate requests carrying other hosts' SameSite cookies and can set parent-domain cookies. Those services must enforce their own CSRF protection and cookie boundaries. tinyhost checks admin POST origins, but cannot protect unrelated services. Serving untrusted content from a separate registrable domain reduces sibling-service exposure; Apps sharing that content origin still are not isolated from each other.

These are documented deployment trade-offs, not a guarantee that hostile content is safe. Bypasses of the Bearer/admin write boundary, cross-host session protections, upload path restrictions or preview expiry are appropriate subjects for a private security report.
