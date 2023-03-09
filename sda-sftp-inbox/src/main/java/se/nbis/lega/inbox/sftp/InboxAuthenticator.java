package se.nbis.lega.inbox.sftp;

import com.github.benmanes.caffeine.cache.Caffeine;
import com.github.benmanes.caffeine.cache.Expiry;
import com.github.benmanes.caffeine.cache.LoadingCache;
import lombok.extern.slf4j.Slf4j;
import org.apache.commons.codec.binary.Base64;
import org.apache.commons.codec.digest.Crypt;
import org.apache.sshd.common.config.keys.KeyUtils;
import org.apache.sshd.common.config.keys.PublicKeyEntryDecoder;
import org.apache.sshd.server.auth.password.PasswordAuthenticator;
import org.apache.sshd.server.auth.password.PasswordChangeRequiredException;
import org.apache.sshd.server.auth.pubkey.PublickeyAuthenticator;
import org.apache.sshd.server.session.ServerSession;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.security.crypto.bcrypt.BCrypt;
import org.springframework.stereotype.Component;
import org.springframework.util.ObjectUtils;
import org.springframework.util.StringUtils;
import se.nbis.lega.inbox.pojo.Credentials;

import java.io.IOException;
import java.security.GeneralSecurityException;
import java.security.PublicKey;
import java.util.List;
import java.util.concurrent.TimeUnit;

/**
 * Component that authenticates users against the inbox.
 */
@Slf4j
@Component
public class InboxAuthenticator implements PublickeyAuthenticator, PasswordAuthenticator {

    private long defaultCacheTTL;
    private CredentialsProvider credentialsProvider;

    // Caffeine cache with entry-specific TTLs
    private final LoadingCache<String, Credentials> credentialsCache = Caffeine.newBuilder()
            .expireAfter(new Expiry<String, Credentials>() {
                public long expireAfterCreate(String key, Credentials graph, long currentTime) {
                    return TimeUnit.SECONDS.toNanos(defaultCacheTTL);
                }

                public long expireAfterUpdate(String key, Credentials graph, long currentTime, long currentDuration) {
                    return TimeUnit.SECONDS.toNanos(defaultCacheTTL);
                }

                public long expireAfterRead(String key, Credentials graph, long currentTime, long currentDuration) {
                    return TimeUnit.SECONDS.toNanos(defaultCacheTTL);
                }

            })
            .build(key -> credentialsProvider.getCredentials(key));

    /**
     * {@inheritDoc}
     */
    @Override
    public boolean authenticate(String username, String password, ServerSession session) throws PasswordChangeRequiredException {
        try {
            Credentials credentials = credentialsCache.get(username);
            String hash = credentials.getPasswordHash();
            if (!password.equals("")) {
                return StringUtils.startsWithIgnoreCase(hash, "$2")
                        ? BCrypt.checkpw(password, hash)
                        : ObjectUtils.nullSafeEquals(hash, Crypt.crypt(password, hash));
            }
            log.error("password is empty, cannot login user");
            return false;
        } catch (Exception e) {
            log.error(e.getMessage(), e);
            return false;
        }
    }

    /**
     * {@inheritDoc}
     */
    @Override
    public boolean authenticate(String username, PublicKey key, ServerSession session) {
        try {
            Credentials credentials = credentialsCache.get(username);
            if (credentials.getPublicKey() != null && key != null) {
                List<String> keysList = credentials.getPublicKey();
                for (String pubKey : keysList) {
                    PublicKey publicKey = readKey(pubKey);
                    return KeyUtils.compareKeys(publicKey, key);
                }
            }
            log.error("key is empty, cannot login user");
            return false;
        } catch (Exception e) {
            log.error(e.getMessage(), e);
            return false;
        }
    }

    private PublicKey readKey(String key) throws IOException, GeneralSecurityException {
        String keyType = key.split(" ")[0];
        byte[] keyBytes = Base64.decodeBase64(key.split(" ")[1]);
        PublicKeyEntryDecoder<?, ?> publicKeyEntryDecoder = KeyUtils.getPublicKeyEntryDecoder(keyType);
        PublicKey publicKey = publicKeyEntryDecoder.decodePublicKey(null, keyType, keyBytes, null);
        return publicKey;
    }

    @Value("${inbox.cache.ttl}")
    public void setDefaultCacheTTL(long defaultCacheTTL) {
        this.defaultCacheTTL = defaultCacheTTL;
    }

    @Autowired
    public void setCredentialsProvider(CredentialsProvider credentialsProvider) {
        this.credentialsProvider = credentialsProvider;
    }

}
