const assert = require('assert');
const camelCase = require('camelcase');
const Provider = require('oidc-provider');

const port = process.env.PORT || 3000;
// External port can legally be an empty string
const ext_port = process.env.EXTERNAL_PORT ?? process.env.PORT;
const host = process.env.HOST || "oidc" ;

const config = ['CLIENT_ID', 'CLIENT_SECRET', 'CLIENT_REDIRECT_URI'].reduce((acc, v) => {
  assert(process.env[v], `${v} config missing`);
  acc[camelCase(v)] = process.env[v];
  return acc;
}, {});

const oidcConfig = {

  features: {
    devInteractions: true,
    discovery: true,
    registration: false,
    revocation: true,
    sessionManagement: false
  },
  formats: {
    default: 'jwt',
    AccessToken: 'jwt',
    RefreshToken: 'jwt'
  },
  routes: {
    authorization: process.env.AUTH_ROUTE || '/auth',
    introspection: process.env.INTROSPECTION_ROUTE || '/token/introspection',
    certificates: process.env.JWKS_ROUTE || '/jwks',
    revocation: process.env.REVOCATION_ROUTE ||'/token/revocation',
    token: process.env.TOKEN_ROUTE || '/token',
    userinfo: process.env.USERINFO_ROUTE ||'/userinfo'
  },
   scopes: [
     'openid',
     'ga4gh_passport_v1',
     'profile',
     'email',
     'offline_access'
   ],
    claims: {
      acr: null,
      sid: null,
      ga4gh_passport_v1: ['ga4gh_passport_v1'],
      auth_time: null,
      ss: null,
      openid: [ 'sub' ],
      profile: ['name', 'email']
      },

  findById: async function findById(ctx, sub, token) {
    return {
      accountId: sub,
      async claims(use, scope, claims, rejected) {
        return { name: 'Dummy Tester', email:'dummy.tester@gs.uu.se', sub, ga4gh_passport_v1: ['eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIwIiwibmFtZSI6InRlc3QiLCJnYTRnaF92aXNhX3YxIjp7ImFzc2VydGVkIjoxLCJieSI6InN5c3RlbSIsInNvdXJjZSI6Imh0dHA6Ly93d3cudXUuc2UvZW4vIiwidHlwZSI6IkFmZmlsaWF0aW9uQW5kUm9sZSIsInZhbHVlIjoic3RhZmZAdXUuc2UifSwiYWRtaW4iOnRydWUsImp0aSI6InRlc3QiLCJpYXQiOjE1ODQ4OTc4NDIsImV4cCI6MTU4NDkwMTQ0Mn0.RkAULuJEaExt0zVu3_uE2BSdkHLAHRD8owqhrsrTfLI'] };
      },
    };
  },

};

const oidc = new Provider(`http://${host}${ext_port ? ':' : ''}${ext_port}`, oidcConfig);

const clients= [{
    client_id: config.clientId,
    client_secret: config.clientSecret,
    redirect_uris: config.clientRedirectUri.split(",")
  }];

let server;
(async () => {
await oidc.initialize({ clients });
  server = oidc.listen(port, () => {
    console.log(
      `mock-oidc-user-server listening on port ${port}, check http://${host}:${port}/.well-known/openid-configuration`
    );
  });
})().catch(err => {
  if (server && server.listening) server.close();
  console.error(err);
  process.exitCode = 1;
});
