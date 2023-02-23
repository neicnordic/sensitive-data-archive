import unittest
import requests


class TestElixirAuth(unittest.TestCase):
    """ElixirAuth.
    Testing ElixirAuth."""

    def setUp(self):
        """Initialise authenticator."""
        self.backend_url = "http://localhost:8080/elixir/"


    def tearDown(self):
        """Finalise test."""
        print("Finishing test")


    def test_valid_elixir_login(self):
        """Test that the login endpoint is active."""

        grant_response = requests.get(self.backend_url,
                                allow_redirects=True)

        print("Grant response")
        location = grant_response.url
        grant_id = location.split('/').pop()
        print(grant_id)
        print(location)

        self.assertEqual(grant_response.status_code, 200)
        self.assertIsNotNone(grant_id)

        oidc_url = f'http://oidc:9090/interaction/{grant_id}/submit'
        cookies = {"_grant": grant_id}
        creds_payload = {"view":'login',
                         "login":'dummy@example.com',
                         "password":'dummy',
                         "submit": ''}

        oidc_response = requests.post(oidc_url,
                                allow_redirects=True,
                                data=creds_payload,
                                cookies=cookies)

        location = oidc_response.url
        self.assertEqual(oidc_response.status_code, 200)
        self.assertIs(self.backend_url in location, True)

class TestEGAAuth(unittest.TestCase):
    """EgaAuth.
    Testing EgaAuth."""

    def setUp(self):
        """Initialise authenticator."""
        self.backend_url = "http://localhost:8080/ega"


    def tearDown(self):
        """Finalise test."""
        print("Finishing test")


    def test_valid_ega_login(self):
        """Test that the login is successful."""
        creds_payload = { "username":'dummy@example.com',
                         "password":'dummy',
                         "submit": 'log+in' }

        login_response = requests.post(self.backend_url,
                                       allow_redirects=False,
                                       data=creds_payload,
                                       cookies=None)


        self.assertEqual(login_response.status_code, 200)


    def test_invalid_ega_login(self):
        """Test that the login is not successful."""
        creds_payload = { "username":'dummy@foo.bar',
                         "password":'wrongpassword',
                         "submit": 'log+in' }

        login_response = requests.post(self.backend_url,
                                       allow_redirects=False,
                                       data=creds_payload,
                                       cookies=None)


        self.assertEqual(login_response.status_code, 303)
