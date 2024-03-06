import unittest
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
