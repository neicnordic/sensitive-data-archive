import unittest
from urllib.parse import parse_qs, urlparse

import requests

class TestEGAAuth(unittest.TestCase):
    """Testing EgaAuth."""

    def setUp(self):
        """Initialise authenticator."""
        self.backend_url = "http://auth-cega:8080/ega"


    def tearDown(self):
        """Finalise test."""
        print("Finishing test")


    def test_valid_ega_login(self):
        """Test that the login is successful."""
        creds_payload = { "username":'dummy@example.com', "password":'dummy', "submit": 'log+in' }
        login_response = requests.post(self.backend_url, allow_redirects=False, data=creds_payload, cookies=None)
        self.assertEqual(login_response.status_code, 200)


    def test_invalid_ega_login(self):
        """Test that the login is not successful."""
        creds_payload = { "username":'dummy@foo.bar', "password":'wrongpassword', "submit": 'log+in' }
        login_response = requests.post(self.backend_url, allow_redirects=False, data=creds_payload, cookies=None)
        self.assertEqual(login_response.status_code, 303)


class TestOIDCAuth(unittest.TestCase):
    """Testing the OIDC login."""

    def setUp(self):
        """Initialise authenticator."""
        self.backend_url = "http://auth-aai:8080/oidc"


    def tearDown(self):
        """Finalise test."""
        print("Finishing test")


    def test_oidc_login_requires_configured_acr(self):
        """Test that the login request asks the provider for the configured authentication context."""
        login_response = requests.get(self.backend_url, allow_redirects=False)
        self.assertEqual(login_response.status_code, 302)
        query = parse_qs(urlparse(login_response.headers["Location"]).query)
        self.assertEqual(query["acr_values"], ["https://refeds.org/profile/mfa"])


    def test_oidc_login_keeps_redirect_uri(self):
        """Test that a per request redirect_uri is passed on together with the authentication context."""
        login_response = requests.get(
            self.backend_url, allow_redirects=False, params={"redirect_uri": "http://frontend/callback"}
        )
        self.assertEqual(login_response.status_code, 302)
        query = parse_qs(urlparse(login_response.headers["Location"]).query)
        self.assertEqual(query["redirect_uri"], ["http://frontend/callback"])
        self.assertEqual(query["acr_values"], ["https://refeds.org/profile/mfa"])


    def test_provider_receives_the_authentication_context(self):
        """Test that the authorization server accepts the request and carries the context on."""
        login_response = requests.get(self.backend_url, allow_redirects=False)
        self.assertEqual(login_response.status_code, 302)

        # Follow the redirect to the mocked AAI. It accepts the authorization
        # request instead of rejecting the parameter, and passes acr_values on
        # to its own login flow, which is what proves it received it.
        provider_response = requests.get(login_response.headers["Location"], allow_redirects=False)
        self.assertEqual(provider_response.status_code, 302)
        provider_location = provider_response.headers["Location"]
        self.assertNotIn("error=", provider_location)
        query = parse_qs(urlparse(provider_location).query)
        self.assertEqual(query["acr_values"], ["https://refeds.org/profile/mfa"])
