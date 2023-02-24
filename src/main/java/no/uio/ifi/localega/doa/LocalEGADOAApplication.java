package no.uio.ifi.localega.doa;

import io.minio.MinioClient;
import io.minio.errors.*;
import lombok.extern.slf4j.Slf4j;
import okhttp3.OkHttpClient;
import org.apache.commons.lang3.StringUtils;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.context.annotation.Bean;

import javax.net.ssl.*;
import java.io.IOException;
import java.io.InputStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.GeneralSecurityException;
import java.security.KeyStore;
import java.security.cert.Certificate;
import java.security.cert.CertificateException;
import java.security.cert.CertificateFactory;
import java.util.*;

/**
 * Spring Boot main file containing the application entry-point and all necessary Spring beans configuration.
 */
@Slf4j
@SpringBootApplication
public class LocalEGADOAApplication {

    /**
     * Spring boot entry-point.
     *
     * @param args Command-line arguments.
     */
    public static void main(String[] args) {
        SpringApplication application = new SpringApplication(LocalEGADOAApplication.class);

        Properties properties = new Properties();
        String rootCertPath = System.getenv("ROOT_CERT_PATH");
        String rootCertPass = System.getenv("ROOT_CERT_PASSWORD");
        if (StringUtils.isNotEmpty(rootCertPath) && StringUtils.isNotEmpty(rootCertPass)) {
            properties.put("spring.rabbitmq.ssl.trust-store", "file:" + rootCertPath);
            properties.put("spring.rabbitmq.ssl.trust-store-password", rootCertPass);
        }
        String clientCertPath = System.getenv("CLIENT_CERT_PATH");
        String clientCertPass = System.getenv("CLIENT_CERT_PASSWORD");
        if (StringUtils.isNotEmpty(clientCertPath) && StringUtils.isNotEmpty(clientCertPass)) {
            properties.put("spring.rabbitmq.ssl.key-store", "file:" + clientCertPath);
            properties.put("spring.rabbitmq.ssl.key-store-password", clientCertPass);
        }
        application.setDefaultProperties(properties);

        application.run(args);
    }

    /**
     * Archive Minio Client Spring bean.
     *
     * @return <code>MinioClient</code>
     * @throws GeneralSecurityException In case of SSL/TLS related errors.
     */
    @Bean
    public MinioClient archiveClient(@Value("${s3.endpoint}") String s3Endpoint,
                                   @Value("${s3.port}") int s3Port,
                                   @Value("${s3.access-key}") String s3AccessKey,
                                   @Value("${s3.secret-key}") String s3SecretKey,
                                   @Value("${s3.region}") String s3Region,
                                   @Value("${s3.secure}") boolean s3Secure,
                                   @Value("${s3.root-ca}") String s3RootCA) throws GeneralSecurityException, ServerException, InsufficientDataException, InternalException, IOException, InvalidResponseException, InvalidBucketNameException, XmlParserException, ErrorResponseException, RegionConflictException {
        MinioClient.Builder builder = MinioClient.builder().endpoint(s3Endpoint, s3Port, s3Secure).region(s3Region).credentials(s3AccessKey, s3SecretKey);
        Optional<OkHttpClient> optionalOkHttpClient = buildOkHttpClient(s3RootCA);
        optionalOkHttpClient.ifPresent(builder::httpClient);
        return builder.build();
    }

    /**
     * Outbox Minio Client Spring bean.
     *
     * @return <code>MinioClient</code>
     * @throws GeneralSecurityException In case of SSL/TLS related errors.
     */
    @Bean
    public MinioClient outboxClient(@Value("${s3.out.endpoint}") String s3Endpoint,
                                    @Value("${s3.out.port}") int s3Port,
                                    @Value("${s3.out.access-key}") String s3AccessKey,
                                    @Value("${s3.out.secret-key}") String s3SecretKey,
                                    @Value("${s3.out.region}") String s3Region,
                                    @Value("${s3.out.secure}") boolean s3Secure,
                                    @Value("${s3.out.root-ca}") String s3RootCA) throws GeneralSecurityException, ServerException, InsufficientDataException, InternalException, IOException, InvalidResponseException, InvalidBucketNameException, XmlParserException, ErrorResponseException, RegionConflictException {
        MinioClient.Builder builder = MinioClient.builder().endpoint(s3Endpoint, s3Port, s3Secure).region(s3Region).credentials(s3AccessKey, s3SecretKey);
        Optional<OkHttpClient> optionalOkHttpClient = buildOkHttpClient(s3RootCA);
        optionalOkHttpClient.ifPresent(builder::httpClient);
        return builder.build();
    }

    private Optional<OkHttpClient> buildOkHttpClient(String s3RootCA) throws GeneralSecurityException {
        try {
            X509TrustManager trustManager = trustManagerForCertificates(Files.newInputStream(Path.of(s3RootCA)));
            SSLContext sslContext = SSLContext.getInstance("TLS");
            sslContext.init(null, new TrustManager[]{trustManager}, null);
            return Optional.of(new OkHttpClient.Builder().sslSocketFactory(sslContext.getSocketFactory(), trustManager).build());
        } catch (CertificateException | IOException e) {
            log.warn("S3 Root CA file {} does not exist or can't be opened, skipping...", s3RootCA);
            return Optional.empty();
        }
    }

    private X509TrustManager trustManagerForCertificates(InputStream in) throws GeneralSecurityException {
        CertificateFactory certificateFactory = CertificateFactory.getInstance("X.509");
        Collection<? extends Certificate> certificates = certificateFactory.generateCertificates(in);
        if (certificates.isEmpty()) {
            throw new IllegalArgumentException("Expected non-empty set of trusted certificates");
        }

        // put the certificates into a key store
        char[] password = UUID.randomUUID().toString().toCharArray(); // any password will do
        KeyStore keyStore = newEmptyKeyStore(password);
        for (Certificate certificate : certificates) {
            keyStore.setCertificateEntry(UUID.randomUUID().toString(), certificate);
        }

        // use it to build an X509 trust manager
        KeyManagerFactory keyManagerFactory = KeyManagerFactory.getInstance(KeyManagerFactory.getDefaultAlgorithm());
        keyManagerFactory.init(keyStore, password);
        TrustManagerFactory trustManagerFactory = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm());
        trustManagerFactory.init(keyStore);
        TrustManager[] trustManagers = trustManagerFactory.getTrustManagers();
        if (trustManagers.length != 1 || !(trustManagers[0] instanceof X509TrustManager)) {
            throw new IllegalStateException("Unexpected default trust managers: " + Arrays.toString(trustManagers));
        }
        return (X509TrustManager) trustManagers[0];
    }

    private KeyStore newEmptyKeyStore(char[] password) throws GeneralSecurityException {
        try {
            KeyStore keyStore = KeyStore.getInstance(KeyStore.getDefaultType());
            keyStore.load(null, password);
            return keyStore;
        } catch (IOException e) {
            throw new AssertionError(e);
        }
    }

}
