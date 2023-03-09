package se.nbis.lega.inbox.pojo;

import com.google.gson.annotations.SerializedName;
import lombok.Data;
import lombok.ToString;

/**
 * Nested POJO for MQ message to publish.
 */
@ToString
@Data
public class EncryptedIntegrity {

    @SerializedName("type")
    private final String algorithm;

    @SerializedName("value")
    private final String checksum;

}
