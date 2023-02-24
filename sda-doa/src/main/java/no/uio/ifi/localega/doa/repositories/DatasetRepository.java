package no.uio.ifi.localega.doa.repositories;

import no.uio.ifi.localega.doa.model.LEGADataset;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

import java.util.Collection;

/**
 * Spring Data repository for datasets.
 */
@Repository
public interface DatasetRepository extends JpaRepository<LEGADataset, String> {

    Collection<LEGADataset> findByDatasetId(String datasetId);

    Collection<LEGADataset> findByDatasetIdIn(Collection<String> datasetId);

}
