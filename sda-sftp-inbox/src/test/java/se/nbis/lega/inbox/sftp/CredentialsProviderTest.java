package se.nbis.lega.inbox.sftp;

import org.hamcrest.CoreMatchers;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ErrorCollector;
import org.junit.runner.RunWith;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.http.HttpStatus;
import org.springframework.test.context.junit4.SpringRunner;
import org.springframework.web.client.RestClientException;
import se.nbis.lega.inbox.pojo.Credentials;
import se.nbis.lega.inbox.pojo.KeyAlgorithm;
import se.nbis.lega.inbox.pojo.PasswordHashingAlgorithm;

import java.io.IOException;
import java.net.URISyntaxException;
import java.util.ArrayList;
import java.util.List;

@RunWith(SpringRunner.class)
public class CredentialsProviderTest extends InboxTest {

    @Rule
    public ErrorCollector collector = new ErrorCollector();

    private CredentialsProvider credentialsProvider;

    @Test
    public void getCredentialsSuccess() throws IOException, URISyntaxException {
        Credentials credentials = credentialsProvider.getCredentials(username);
        List<String> publickeyList = new ArrayList<>();
        publickeyList.add(publicKey);
        collector.checkThat(passwordHash, CoreMatchers.is(credentials.getPasswordHash()));
        collector.checkThat(publickeyList, CoreMatchers.is(credentials.getPublicKey()));
    }

    @Test(expected = RestClientException.class)
    public void getCredentialsFail() throws IOException, URISyntaxException {
        mockCEGAEndpoint(username, password, PasswordHashingAlgorithm.BLOWFISH, KeyAlgorithm.RSA, HttpStatus.BAD_REQUEST);
        credentialsProvider.getCredentials(username);
    }

    @Autowired
    public void setCredentialsProvider(CredentialsProvider credentialsProvider) {
        this.credentialsProvider = credentialsProvider;
    }

}
